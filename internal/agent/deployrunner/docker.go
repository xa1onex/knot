package deployrunner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

var imageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,253}(:[a-zA-Z0-9._-]{1,128})?$`)

// DockerRunner uses the docker CLI with fixed argument shapes only.
type DockerRunner struct {
	binary string
}

func NewDockerRunner() *DockerRunner {
	return &DockerRunner{binary: "docker"}
}

func (d *DockerRunner) Available() bool {
	out, err := exec.Command(d.binary, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func (d *DockerRunner) Apply(ctx context.Context, depID, removeCID string, spec protocol.DeploySpec) (string, bool, []string, error) {
	if err := validateDockerSpec(spec); err != nil {
		return "", false, nil, err
	}
	var logs []string
	if removeCID != "" {
		l, _ := d.removeContainer(ctx, removeCID)
		logs = append(logs, l...)
	}
	name := dockerInstanceName(spec.Name, depID)

	args := dockerRunArgs(spec, name)
	out, err := d.run(ctx, args...)
	if err != nil {
		return "", false, logs, err
	}
	cid := strings.TrimSpace(string(out))
	logs = append(logs, "docker: started "+cid)

	ok, _ := waitHealth(ctx, spec.Bind, spec.Port, spec.HealthPath, true)
	logs = append(logs, fmt.Sprintf("docker: health=%v", ok))
	return cid, ok, logs, nil
}

func (d *DockerRunner) Stop(ctx context.Context, spec protocol.DeploySpec, containerID string) (string, []string, error) {
	name := dockerName(spec.Name)
	if containerID != "" && !strings.HasPrefix(containerID, "knot-") {
		name = containerID
	}
	out, err := d.run(ctx, "stop", name)
	logs := []string{"docker: stop " + strings.TrimSpace(string(out))}
	return containerID, logs, err
}

func (d *DockerRunner) Restart(ctx context.Context, depID string, spec protocol.DeploySpec, containerID string) (string, bool, []string, error) {
	name := dockerName(spec.Name)
	if containerID != "" && !strings.HasPrefix(containerID, spec.Name) {
		name = containerID
	}
	out, err := d.run(ctx, "restart", name)
	logs := []string{"docker: restart " + strings.TrimSpace(string(out))}
	if err != nil {
		return containerID, false, logs, err
	}
	ok, _ := waitHealth(ctx, spec.Bind, spec.Port, spec.HealthPath, true)
	return containerID, ok, logs, nil
}

func (d *DockerRunner) Remove(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	if containerID == "" {
		containerID = dockerName(spec.Name)
	}
	return d.removeContainer(ctx, containerID)
}

func (d *DockerRunner) Logs(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	name := dockerName(spec.Name)
	if containerID != "" {
		name = containerID
	}
	out, err := d.run(ctx, "logs", "--tail", "100", name)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return []string{"(empty)"}, nil
	}
	return strings.Split(raw, "\n"), nil
}

func (d *DockerRunner) removeContainer(ctx context.Context, id string) ([]string, error) {
	out, err := d.run(ctx, "rm", "-f", id)
	return []string{"docker: rm " + strings.TrimSpace(string(out))}, err
}

func (d *DockerRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, d.binary, args...)
	return cmd.CombinedOutput()
}

func dockerName(service string) string {
	return "knot-" + service
}

func dockerInstanceName(service, depID string) string {
	base := dockerName(service)
	if depID == "" {
		return base
	}
	short := depID
	if len(short) > 8 {
		short = short[:8]
	}
	name := base + "-" + short
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func dockerRunArgs(spec protocol.DeploySpec, containerName string) []string {
	args := []string{
		"run", "-d", "--name", containerName,
		"--network", "bridge",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", fmt.Sprintf("%d", pidsLimit(spec.Limits.Pids)),
		"--memory", fmt.Sprintf("%dm", memLimit(spec.Limits.MemoryMB)),
		"--cpus", fmt.Sprintf("%.2f", cpuLimit(spec.Limits.CPUs)),
		"-p", fmt.Sprintf("%s:%d:%d", spec.Bind, spec.Port, spec.Port),
	}
	if spec.Limits.ReadOnly {
		args = append(args, "--read-only")
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	return append(args, spec.Image)
}

func cpuLimit(n float64) float64 {
	if n <= 0 {
		return protocol.DefaultDeployCPUs
	}
	return n
}

func memLimit(mb int64) int64 {
	if mb <= 0 {
		return protocol.DefaultDeployMemoryMB
	}
	return mb
}

func pidsLimit(n int64) int64 {
	if n <= 0 {
		return protocol.DefaultDeployPids
	}
	return n
}

func validateDockerSpec(spec protocol.DeploySpec) error {
	if spec.Runtime != "docker" {
		return fmt.Errorf("unsupported runtime %q", spec.Runtime)
	}
	if !imageRe.MatchString(spec.Image) {
		return fmt.Errorf("invalid image reference")
	}
	if spec.Bind != "127.0.0.1" && spec.Bind != "::1" {
		return fmt.Errorf("bind must be loopback")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return fmt.Errorf("invalid port")
	}
	return nil
}
