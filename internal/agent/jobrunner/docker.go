package jobrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

var imageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,253}(:[a-zA-Z0-9._-]{1,128})?$`)

type DockerRunner struct {
	StorageRoot string
	Policy      Policy
	WorkRoot    string
	binary      string
}

func NewDockerRunner(root string, policy Policy) *DockerRunner {
	return &DockerRunner{StorageRoot: root, Policy: policy, binary: "docker"}
}

func (d *DockerRunner) Available() bool {
	out, err := exec.Command(d.binary, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func (d *DockerRunner) Run(ctx context.Context, spec protocol.JobSpec, emit func(stream, line string)) (protocol.JobResult, error) {
	res := protocol.JobResult{JobID: spec.JobID, OutputPath: spec.OutputPath}
	if reason, msg := d.Policy.Check(spec); reason != "" {
		return rejectedResult(spec, reason, msg), nil
	}
	if err := validateJobSpec(spec); err != nil {
		res.Status = protocol.JobStatusFailed
		res.Error = err.Error()
		return res, err
	}
	work, inDir, outDir, err := workDirs(d.WorkRoot, spec.JobID)
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(work)
	if err := stageJobInput(d.StorageRoot, spec.JobID, spec.SourcePath, inDir); err != nil {
		res.Status = protocol.JobStatusFailed
		res.Error = err.Error()
		cleanupJobOutput(d.StorageRoot, spec.JobID)
		return res, err
	}

	name := dockerJobName(spec.JobID)
	_, _ = d.run(ctx, "rm", "-f", name)

	bound := d.Policy.Bound(spec.Resources)
	diskBytes := bound.DiskMB << 20
	var exceeded atomic.Bool
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	go watchDirSize(watchCtx, outDir, diskBytes, func() {
		exceeded.Store(true)
		_, _ = d.run(context.Background(), "kill", name)
	})

	args := dockerJobArgs(spec, name, inDir, outDir, d.Policy)
	cmd := exec.CommandContext(ctx, d.binary, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if text != "" {
		for _, line := range strings.Split(text, "\n") {
			emit("stdout", line)
			res.LogLines = append(res.LogLines, line)
		}
	}

	oom := d.oomKilled(name)
	_, _ = d.run(context.Background(), "rm", "-f", name)

	if exceeded.Load() || dirSize(outDir) > diskBytes {
		res.Status = protocol.JobStatusFailed
		res.Reason = protocol.JobReasonResourceExceeded
		res.Error = "resource_exceeded: disk limit"
		res.ExitCode = intPtr(137)
		res.ContainerID = name
		cleanupJobOutput(d.StorageRoot, spec.JobID)
		return res, nil
	}
	if oom {
		res.Status = protocol.JobStatusFailed
		res.Reason = protocol.JobReasonResourceExceeded
		res.Error = "resource_exceeded: OOM"
		res.ExitCode = intPtr(137)
		res.ContainerID = name
		cleanupJobOutput(d.StorageRoot, spec.JobID)
		return res, nil
	}

	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if ctx.Err() != nil {
			_, _ = d.run(context.Background(), "kill", name)
			_, _ = d.run(context.Background(), "rm", "-f", name)
			if ctx.Err() == context.DeadlineExceeded {
				res.Status = protocol.JobStatusTimeout
				res.Error = "job timed out"
			} else {
				res.Status = protocol.JobStatusCanceled
				res.Error = "job canceled"
			}
			res.ExitCode = intPtr(137)
			res.ContainerID = name
			cleanupJobOutput(d.StorageRoot, spec.JobID)
			return res, nil
		} else {
			res.Status = protocol.JobStatusFailed
			res.Error = err.Error()
			cleanupJobOutput(d.StorageRoot, spec.JobID)
			return res, err
		}
	}
	res.ExitCode = intPtr(code)
	res.ContainerID = name
	if code != 0 {
		res.Status = protocol.JobStatusFailed
		if code == 137 {
			res.Reason = protocol.JobReasonResourceExceeded
			res.Error = "resource_exceeded"
		} else {
			res.Error = fmt.Sprintf("exit %d", code)
		}
		cleanupJobOutput(d.StorageRoot, spec.JobID)
		return res, nil
	}
	arts, err := commitJobOutput(d.StorageRoot, spec, outDir, d.Policy)
	if err != nil {
		cleanupJobOutput(d.StorageRoot, spec.JobID)
		if errors.Is(err, errArtifactLimit) {
			fail := failedArtifactLimit(spec, err)
			fail.LogLines = res.LogLines
			fail.ContainerID = name
			fail.ExitCode = intPtr(code)
			return fail, nil
		}
		res.Status = protocol.JobStatusFailed
		res.Error = err.Error()
		return res, err
	}
	applyCommit(&res, spec, arts)
	res.ContainerID = name
	return res, nil
}

func (d *DockerRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, d.binary, args...)
	return cmd.CombinedOutput()
}

func (d *DockerRunner) oomKilled(name string) bool {
	out, err := d.run(context.Background(), "inspect", "-f", "{{.State.OOMKilled}}", name)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func dockerJobName(jobID string) string {
	id := jobID
	if len(id) > 12 {
		id = id[:12]
	}
	return "knot-job-" + id
}

func dockerJobArgs(spec protocol.JobSpec, name, inDir, outDir string, pol Policy) []string {
	bound := pol.Bound(spec.Resources)
	args := []string{
		"run", "--name", name,
		"--network", "none",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", strconv.FormatInt(bound.Pids, 10),
		"--memory", fmt.Sprintf("%dm", bound.MemoryMB),
		"--memory-swap", fmt.Sprintf("%dm", bound.MemoryMB),
		"--cpus", fmt.Sprintf("%.2f", bound.CPU),
		"--tmpfs", fmt.Sprintf("/tmp:rw,noexec,nosuid,size=%dm", bound.DiskMB),
		"-v", inDir + ":/input:ro",
		"-v", outDir + ":/output",
	}
	if spec.Resources.GPU > 0 && pol.GPURuntimeOK && pol.MaxGPU > 0 {
		n := spec.Resources.GPU
		if n > pol.MaxGPU {
			n = pol.MaxGPU
		}
		devices := make([]string, n)
		for i := 0; i < n; i++ {
			devices[i] = strconv.Itoa(i)
		}
		args = append(args, "--gpus", "device="+strings.Join(devices, ","))
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}

func validateJobSpec(spec protocol.JobSpec) error {
	if !imageRe.MatchString(spec.Image) {
		return fmt.Errorf("invalid image reference")
	}
	if len(spec.Command) > protocol.MaxJobArgs {
		return fmt.Errorf("too many command args")
	}
	if len(spec.Command) >= 2 {
		base := strings.ToLower(filepath.Base(spec.Command[0]))
		switch base {
		case "sh", "bash", "zsh", "dash", "cmd.exe", "powershell", "pwsh":
			if spec.Command[1] == "-c" || spec.Command[1] == "/c" || spec.Command[1] == "-Command" {
				return fmt.Errorf("arbitrary shell is not allowed")
			}
		}
	}
	return nil
}

func watchDirSize(ctx context.Context, dir string, limit int64, onExceed func()) {
	if limit <= 0 {
		return
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if dirSize(dir) > limit {
				onExceed()
				return
			}
		}
	}
}

func dirSize(root string) int64 {
	var n int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		n += info.Size()
		return nil
	})
	return n
}
