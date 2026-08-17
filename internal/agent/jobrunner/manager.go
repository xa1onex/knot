package jobrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/pkg/protocol"
)

type Runner interface {
	Run(ctx context.Context, spec protocol.JobSpec, emit func(stream, line string)) (protocol.JobResult, error)
}

type Manager struct {
	Send        func(v any) error
	StorageRoot string
	WorkRoot    string
	Runner      Runner
	Policy      Policy

	mu      sync.Mutex
	running int
	cancels map[string]context.CancelFunc
}

func NewManager(send func(v any) error, storageRoot string) *Manager {
	pol := LoadPolicy()
	workRoot := defaultWorkRoot(storageRoot)
	sweepLeftovers(storageRoot, workRoot)
	return &Manager{
		Send:        send,
		StorageRoot: storageRoot,
		WorkRoot:    workRoot,
		Policy:      pol,
		Runner:      NewComposite(storageRoot, pol, workRoot),
		cancels:     map[string]context.CancelFunc{},
	}
}

func (m *Manager) Handle(raw []byte) {
	var env struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return
	}
	switch env.Type {
	case protocol.TypeJobRun:
		var run protocol.JobRun
		if json.Unmarshal(raw, &run) != nil {
			return
		}
		timeout := time.Duration(run.Spec.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = time.Duration(protocol.DefaultJobTimeout) * time.Second
		}
		max := time.Duration(m.Policy.normalized().MaxTimeoutSec) * time.Second
		if max > 0 && timeout > max {
			timeout = max
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		m.mu.Lock()
		m.cancels[run.JobID] = cancel
		m.mu.Unlock()
		go m.run(ctx, cancel, run)
	case protocol.TypeJobCancel:
		var c protocol.JobCancel
		if json.Unmarshal(raw, &c) != nil {
			return
		}
		m.cancel(c.JobID)
	}
}

func (m *Manager) cancel(jobID string) {
	m.mu.Lock()
	fn := m.cancels[jobID]
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, run protocol.JobRun) {
	spec := run.Spec
	spec.JobID = run.JobID
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, run.JobID)
		m.mu.Unlock()
	}()

	emit := func(stream, line string) {
		line = deploy.SanitizeLogLine(line)
		_ = m.Send(protocol.JobLogLine{
			Type: protocol.TypeJobLogLine, JobID: run.JobID, Stream: stream, Message: line,
		})
	}

	sendRes := func(res protocol.JobResult) {
		res.Type = protocol.TypeJobResult
		res.RequestID = run.RequestID
		res.JobID = run.JobID
		for i, line := range res.LogLines {
			res.LogLines[i] = deploy.SanitizeLogLine(line)
		}
		if res.Error != "" {
			res.Error = deploy.SanitizeLogLine(res.Error)
		}
		_ = m.Send(res)
	}

	if reason, msg := m.Policy.Check(spec); reason != "" {
		sendRes(rejectedResult(spec, reason, msg))
		return
	}
	if !m.acquire() {
		sendRes(rejectedResult(spec, protocol.JobReasonSlotUnavailable, "slot_unavailable: max concurrent jobs on this node"))
		return
	}
	defer m.release()

	res, err := m.Runner.Run(ctx, spec, emit)
	if err != nil && res.Error == "" {
		res.Error = err.Error()
	}
	if ctx.Err() == context.DeadlineExceeded && !protocol.JobSucceeded(res.Status) && res.Reason != protocol.JobReasonResourceExceeded && res.Reason != protocol.JobReasonArtifactLimit {
		res.Status = protocol.JobStatusTimeout
		res.OK = false
		if res.Error == "" {
			res.Error = "job timed out"
		}
		res.Artifacts = nil
		res.OutputFiles = nil
		cleanupJobOutput(m.StorageRoot, run.JobID)
	} else if ctx.Err() == context.Canceled && !protocol.JobSucceeded(res.Status) && res.Reason != protocol.JobReasonResourceExceeded && res.Reason != protocol.JobReasonArtifactLimit {
		if res.Status != protocol.JobStatusFailed {
			res.Status = protocol.JobStatusCanceled
			res.OK = false
			if res.Error == "" {
				res.Error = "job canceled"
			}
		}
		res.Artifacts = nil
		res.OutputFiles = nil
		cleanupJobOutput(m.StorageRoot, run.JobID)
	} else if !protocol.JobSucceeded(res.Status) {
		cleanupJobOutput(m.StorageRoot, run.JobID)
	}
	sendRes(res)
}

func (m *Manager) acquire() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	max := m.Policy.normalized().MaxConcurrent
	if max > 0 && m.running >= max {
		return false
	}
	m.running++
	return true
}

func (m *Manager) release() {
	m.mu.Lock()
	if m.running > 0 {
		m.running--
	}
	m.mu.Unlock()
}

type Composite struct {
	fake   *FakeRunner
	docker *DockerRunner
	policy Policy
}

func NewComposite(storageRoot string, policy Policy, workRoot string) *Composite {
	return &Composite{
		fake:   &FakeRunner{StorageRoot: storageRoot, Policy: policy, WorkRoot: workRoot},
		docker: &DockerRunner{StorageRoot: storageRoot, Policy: policy, WorkRoot: workRoot, binary: "docker"},
		policy: policy,
	}
}

func isFakeJob(image string) bool {
	return strings.HasPrefix(image, "knot-fake-job")
}

func (c *Composite) Run(ctx context.Context, spec protocol.JobSpec, emit func(stream, line string)) (protocol.JobResult, error) {
	if isFakeJob(spec.Image) {
		return c.fake.Run(ctx, spec, emit)
	}
	if c.docker != nil && c.docker.Available() {
		return c.docker.Run(ctx, spec, emit)
	}
	return c.fake.Run(ctx, spec, emit)
}

func workDirs(workRoot, jobID string) (work, input, output string, err error) {
	prefix := "job-"
	if jobID != "" {
		prefix = jobID[:min(8, len(jobID))] + "-"
	}
	if workRoot != "" {
		if err := os.MkdirAll(workRoot, 0o700); err != nil {
			return "", "", "", err
		}
		work, err = os.MkdirTemp(workRoot, prefix)
	} else {
		work, err = os.MkdirTemp("", "knot-job-"+prefix)
	}
	if err != nil {
		return "", "", "", err
	}
	input = filepath.Join(work, "input")
	output = filepath.Join(work, "output")
	if err := os.MkdirAll(input, 0o700); err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return "", "", "", err
	}
	return work, input, output, nil
}

func intPtr(n int) *int { return &n }

func statusFromExit(code int) string {
	if code == 0 {
		return protocol.JobStatusSucceeded
	}
	return protocol.JobStatusFailed
}
