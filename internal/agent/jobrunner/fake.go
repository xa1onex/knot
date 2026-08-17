package jobrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

type FakeRunner struct {
	StorageRoot string
	Policy      Policy
	WorkRoot    string
}

func NewFakeRunner(root string) *FakeRunner {
	return &FakeRunner{StorageRoot: root, Policy: TestPolicy()}
}

func (f *FakeRunner) Run(ctx context.Context, spec protocol.JobSpec, emit func(stream, line string)) (protocol.JobResult, error) {
	res := protocol.JobResult{JobID: spec.JobID, OutputPath: spec.OutputPath}
	work, inDir, outDir, err := workDirs(f.WorkRoot, spec.JobID)
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(work)

	if err := stageJobInput(f.StorageRoot, spec.JobID, spec.SourcePath, inDir); err != nil {
		res.Status = protocol.JobStatusFailed
		res.Error = err.Error()
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		return res, err
	}

	emit("stdout", "fake: starting "+spec.Image)
	res.LogLines = append(res.LogLines, "fake: starting "+spec.Image)

	lower := strings.ToLower(spec.Image)
	switch {
	case strings.Contains(lower, "fail"):
		emit("stderr", "fake: boom")
		res.LogLines = append(res.LogLines, "fake: boom")
		res.Status = protocol.JobStatusFailed
		res.ExitCode = intPtr(1)
		res.Error = "job failed"
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		return res, nil
	case strings.Contains(lower, "oom"):
		emit("stderr", "fake: OOM")
		res.Status = protocol.JobStatusFailed
		res.Reason = protocol.JobReasonResourceExceeded
		res.Error = "resource_exceeded: OOM"
		res.ExitCode = intPtr(137)
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		return res, nil
	case strings.Contains(lower, "disk"):
		limit := spec.Resources.DiskMB
		if limit <= 0 {
			limit = protocol.DefaultJobDiskMB
		}
		payload := bytes.Repeat([]byte("x"), int(limit<<20)+4096)
		_ = os.WriteFile(filepath.Join(outDir, "huge.bin"), payload, 0o600)
		res.Status = protocol.JobStatusFailed
		res.Reason = protocol.JobReasonResourceExceeded
		res.Error = "resource_exceeded: disk limit"
		res.ExitCode = intPtr(137)
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		return res, nil
	case strings.Contains(lower, "sleep") || strings.Contains(lower, "hang"):
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				res.Status = protocol.JobStatusTimeout
				res.Error = "job timed out"
			} else {
				res.Status = protocol.JobStatusCanceled
				res.Error = "job canceled"
			}
			res.ExitCode = intPtr(137)
			cleanupJobOutput(f.StorageRoot, spec.JobID)
			return res, nil
		case <-time.After(2 * time.Minute):
			res.Status = protocol.JobStatusSucceeded
			res.ExitCode = intPtr(0)
			res.OK = true
			return f.commit(spec, res, outDir)
		}
	case strings.Contains(lower, "manyfiles"):
		for i := 0; i < protocol.DefaultMaxArtifactFiles+1; i++ {
			_ = os.WriteFile(filepath.Join(outDir, fmt.Sprintf("f-%04d.txt", i)), []byte("x"), 0o600)
		}
	case strings.Contains(lower, "bigfile"):
		maxBytes, _, _, _, _ := artifactLimits(f.Policy, spec)
		_ = os.WriteFile(filepath.Join(outDir, "huge.bin"), bytes.Repeat([]byte("x"), int(maxBytes)+1), 0o600)
	case strings.Contains(lower, "deep"):
		p := outDir
		for i := 0; i < protocol.DefaultMaxArtifactDepth+1; i++ {
			p = filepath.Join(p, fmt.Sprintf("d%d", i+1))
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			res.Status = protocol.JobStatusFailed
			res.Error = err.Error()
			cleanupJobOutput(f.StorageRoot, spec.JobID)
			return res, err
		}
		_ = os.WriteFile(filepath.Join(p, "x.txt"), []byte("x"), 0o600)
	case strings.Contains(lower, "secret"):
		line := "hello"
		for k, v := range spec.Env {
			line = fmt.Sprintf("%s=%s", k, v)
			emit("stdout", line)
			res.LogLines = append(res.LogLines, line)
		}
		emit("stdout", "hello")
		res.LogLines = append(res.LogLines, "hello")
		writeDefaultOutput(inDir, outDir)
	default:
		emit("stdout", "hello")
		res.LogLines = append(res.LogLines, "hello")
		writeDefaultOutput(inDir, outDir)
	}

	disk := spec.Resources.DiskMB
	if disk <= 0 {
		disk = protocol.DefaultJobDiskMB
	}
	if dirSize(outDir) > disk<<20 {
		res.Status = protocol.JobStatusFailed
		res.Reason = protocol.JobReasonResourceExceeded
		res.Error = "resource_exceeded: disk limit"
		res.ExitCode = intPtr(137)
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		return res, nil
	}

	return f.commit(spec, res, outDir)
}

func (f *FakeRunner) commit(spec protocol.JobSpec, res protocol.JobResult, outDir string) (protocol.JobResult, error) {
	arts, err := commitJobOutput(f.StorageRoot, spec, outDir, f.Policy)
	if err != nil {
		cleanupJobOutput(f.StorageRoot, spec.JobID)
		if errors.Is(err, errArtifactLimit) {
			fail := failedArtifactLimit(spec, err)
			fail.LogLines = res.LogLines
			return fail, nil
		}
		res.Status = protocol.JobStatusFailed
		res.Error = err.Error()
		res.OK = false
		return res, err
	}
	applyCommit(&res, spec, arts)
	return res, nil
}

func writeDefaultOutput(inDir, outDir string) {
	payload := []byte("hello\n")
	_ = os.WriteFile(filepath.Join(outDir, "result.txt"), payload, 0o600)
	sum := sha256.Sum256(payload)
	meta, _ := json.Marshal(map[string]string{"sha256": hex.EncodeToString(sum[:])})
	_ = os.WriteFile(filepath.Join(outDir, "hash.json"), meta, 0o600)
	if entries, err := os.ReadDir(inDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(inDir, e.Name()))
			if err != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(outDir, e.Name()), b, 0o600)
		}
	}
}
