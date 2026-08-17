package buildrunner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/knot-infra/knot/pkg/protocol"
)

type DockerRunner struct {
	git    string
	docker string
	root   string
}

func NewDockerRunner(workRoot string) *DockerRunner {
	return &DockerRunner{git: "git", docker: "docker", root: workRoot}
}

func (d *DockerRunner) Available() bool {
	out, err := exec.Command(d.docker, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func (d *DockerRunner) Run(ctx context.Context, spec protocol.BuildSpec, emit func(stream, line string), progress func(status string)) protocol.BuildResult {
	res := protocol.BuildResult{BuildID: spec.BuildID, Image: spec.Tag}
	if err := validateBuildSpec(spec); err != nil {
		res.Status = protocol.BuildStatusFailedBuild
		res.Error = err.Error()
		return res
	}

	work, err := os.MkdirTemp(d.root, "knot-build-")
	if err != nil {
		res.Status = protocol.BuildStatusFailedClone
		res.Error = "workdir: " + err.Error()
		return res
	}
	defer os.RemoveAll(work)
	dest := filepath.Join(work, "src")

	progress(protocol.BuildStatusCloning)
	emit("stdout", "cloning "+spec.URL)
	if err := d.clone(ctx, spec, dest, emit); err != nil {
		res.Status = protocol.BuildStatusFailedClone
		res.Error = err.Error()
		return res
	}
	rev, _ := d.revParse(ctx, dest)
	res.Revision = rev

	progress(protocol.BuildStatusBuilding)
	emit("stdout", "docker build -f "+spec.Dockerfile+" -t "+spec.Tag+" "+spec.Context)
	if err := d.build(ctx, spec, dest, emit); err != nil {
		res.Status = protocol.BuildStatusFailedBuild
		res.Error = err.Error()
		return res
	}

	progress(protocol.BuildStatusPushing)
	emit("stdout", "docker push "+spec.Tag)
	if spec.RegistryToken != "" {
		if err := d.login(ctx, spec, emit); err != nil {
			res.Status = protocol.BuildStatusFailedPush
			res.Error = err.Error()
			return res
		}
	}
	if err := d.stream(ctx, emit, nil, d.docker, "push", spec.Tag); err != nil {
		res.Status = protocol.BuildStatusFailedPush
		res.Error = err.Error()
		return res
	}

	res.OK = true
	res.Status = protocol.BuildStatusCompleted
	res.Image = spec.Tag
	emit("stdout", "built "+spec.Tag)
	return res
}

func (d *DockerRunner) clone(ctx context.Context, spec protocol.BuildSpec, dest string, emit func(stream, line string)) error {
	cloneURL, err := authenticatedGitURL(spec.URL, spec.GitToken)
	if err != nil {
		return err
	}
	args := []string{"clone", "--depth", "1"}
	ref := spec.Branch
	if spec.GitTag != "" {
		ref = spec.GitTag
	}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, cloneURL, dest)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true")
	if err := d.streamEnv(ctx, emit, env, d.git, args...); err != nil {
		return err
	}
	if spec.Revision != "" {
		return d.streamEnv(ctx, emit, env, d.git, "-C", dest, "checkout", spec.Revision)
	}
	return nil
}

func (d *DockerRunner) revParse(ctx context.Context, dest string) (string, error) {
	out, err := exec.CommandContext(ctx, d.git, "-C", dest, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *DockerRunner) build(ctx context.Context, spec protocol.BuildSpec, dest string, emit func(stream, line string)) error {
	df := filepath.Join(dest, filepath.FromSlash(spec.Dockerfile))
	ctxDir := filepath.Join(dest, filepath.FromSlash(spec.Context))
	return d.stream(ctx, emit, nil, d.docker, "build", "-f", df, "-t", spec.Tag, ctxDir)
}

func (d *DockerRunner) login(ctx context.Context, spec protocol.BuildSpec, emit func(stream, line string)) error {
	host := registryHost(spec.Tag)
	user := spec.RegistryUser
	if user == "" {
		user = "token"
	}
	cmd := exec.CommandContext(ctx, d.docker, "login", "--username", user, "--password-stdin", host)
	cmd.Stdin = strings.NewReader(spec.RegistryToken)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			emit("stdout", line)
		}
	}
	if err != nil {
		return fmt.Errorf("docker login: %w", err)
	}
	return nil
}

func (d *DockerRunner) stream(ctx context.Context, emit func(stream, line string), extraEnv []string, name string, args ...string) error {
	return d.streamEnv(ctx, emit, extraEnv, name, args...)
}

func (d *DockerRunner) streamEnv(ctx context.Context, emit func(stream, line string), extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if extraEnv != nil {
		cmd.Env = extraEnv
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{}, 2)
	go scanLines(stdout, "stdout", emit, done)
	go scanLines(stderr, "stderr", emit, done)
	err = cmd.Wait()
	<-done
	<-done
	if err != nil {
		return err
	}
	return nil
}

func scanLines(r io.Reader, stream string, emit func(stream, line string), done chan struct{}) {
	defer func() { done <- struct{}{} }()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		emit(stream, sc.Text())
	}
}

func authenticatedGitURL(raw, token string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if token == "" {
		return raw, nil
	}
	if u.User != nil {
		return "", fmt.Errorf("credentials must not be in url")
	}
	if user, pass, ok := strings.Cut(token, ":"); ok && user != "" && pass != "" {
		u.User = url.UserPassword(user, pass)
	} else {
		u.User = url.UserPassword("x-access-token", token)
	}
	return u.String(), nil
}

func registryHost(tag string) string {
	host := tag
	if i := strings.IndexByte(tag, '/'); i > 0 {
		host = tag[:i]
	} else {
		return "docker.io"
	}
	if strings.Contains(host, ".") || strings.Contains(host, ":") || host == "localhost" {
		if j := strings.IndexByte(host, '/'); j > 0 {
			return host[:j]
		}
		return host
	}
	return "docker.io"
}

func validateBuildSpec(spec protocol.BuildSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("tag required")
	}
	df := filepath.ToSlash(spec.Dockerfile)
	cx := filepath.ToSlash(spec.Context)
	if df == "" || strings.Contains(df, "..") || filepath.IsAbs(df) {
		return fmt.Errorf("invalid dockerfile")
	}
	if cx == "" || strings.Contains(cx, "..") || filepath.IsAbs(cx) {
		return fmt.Errorf("invalid context")
	}
	return nil
}

type Composite struct {
	fake   *FakeRunner
	docker *DockerRunner
}

func NewComposite(workRoot string) *Composite {
	return &Composite{fake: NewFakeRunner(), docker: NewDockerRunner(workRoot)}
}

func isFakeGit(url string) bool {
	return strings.HasPrefix(url, "knot-fake-git:")
}

func (c *Composite) Run(ctx context.Context, spec protocol.BuildSpec, emit func(stream, line string), progress func(status string)) protocol.BuildResult {
	if isFakeGit(spec.URL) {
		return c.fake.Run(ctx, spec, emit, progress)
	}
	if c.docker != nil && c.docker.Available() {
		return c.docker.Run(ctx, spec, emit, progress)
	}
	return protocol.BuildResult{
		BuildID: spec.BuildID,
		Status:  protocol.BuildStatusFailedBuild,
		Error:   "docker unavailable",
	}
}
