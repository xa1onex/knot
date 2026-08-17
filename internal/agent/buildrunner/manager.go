package buildrunner

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/pkg/protocol"
)

type Runner interface {
	Run(ctx context.Context, spec protocol.BuildSpec, emit func(stream, line string), progress func(status string)) protocol.BuildResult
}

type Manager struct {
	Send   func(v any) error
	Runner Runner

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewManager(send func(v any) error, workRoot string) *Manager {
	return &Manager{
		Send:    send,
		Runner:  NewComposite(workRoot),
		cancels: map[string]context.CancelFunc{},
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
	case protocol.TypeBuildRun:
		var run protocol.BuildRun
		if json.Unmarshal(raw, &run) != nil {
			return
		}
		timeout := time.Duration(run.Spec.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = time.Duration(protocol.DefaultBuildTimeout) * time.Second
		}
		max := time.Duration(protocol.MaxBuildTimeout) * time.Second
		if timeout > max {
			timeout = max
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		m.mu.Lock()
		m.cancels[run.BuildID] = cancel
		m.mu.Unlock()
		go m.run(ctx, cancel, run)
	case protocol.TypeBuildCancel:
		var c protocol.BuildCancel
		if json.Unmarshal(raw, &c) != nil {
			return
		}
		m.cancel(c.BuildID)
	}
}

func (m *Manager) cancel(id string) {
	m.mu.Lock()
	fn := m.cancels[id]
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, run protocol.BuildRun) {
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, run.BuildID)
		m.mu.Unlock()
	}()

	spec := run.Spec
	spec.BuildID = run.BuildID
	secrets := compact(spec.GitToken, spec.RegistryToken, spec.RegistryUser)

	emit := func(stream, line string) {
		line = deploy.RedactSecrets(line, secrets)
		_ = m.Send(protocol.BuildLogLine{
			Type: protocol.TypeBuildLogLine, BuildID: run.BuildID, Stream: stream, Message: line,
		})
	}
	progress := func(status string) {
		_ = m.Send(protocol.BuildProgress{
			Type: protocol.TypeBuildProgress, BuildID: run.BuildID, Status: status,
		})
	}

	res := m.Runner.Run(ctx, spec, emit, progress)
	res.Type = protocol.TypeBuildResult
	res.RequestID = run.RequestID
	res.BuildID = run.BuildID
	for i, line := range res.LogLines {
		res.LogLines[i] = deploy.RedactSecrets(line, secrets)
	}
	if res.Error != "" {
		res.Error = deploy.RedactSecrets(res.Error, secrets)
	}
	if ctx.Err() == context.DeadlineExceeded && !res.OK {
		res.Status = protocol.BuildStatusFailed
		if res.Error == "" {
			res.Error = "build timed out"
		}
	} else if ctx.Err() == context.Canceled && !res.OK {
		if res.Status == "" || !protocol.BuildTerminal(res.Status) {
			res.Status = protocol.BuildStatusCanceled
		}
		if res.Error == "" {
			res.Error = "build canceled"
		}
	}
	_ = m.Send(res)
}

func compact(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
