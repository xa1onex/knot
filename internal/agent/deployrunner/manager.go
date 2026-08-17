package deployrunner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knot-infra/knot/pkg/protocol"
)

// Runner applies structured deploy specs to a sandboxed workload (no arbitrary shell).
type Runner interface {
	Apply(ctx context.Context, depID string, removeContainerID string, spec protocol.DeploySpec) (containerID string, healthOK bool, logs []string, err error)
	Stop(ctx context.Context, spec protocol.DeploySpec, containerID string) (string, []string, error)
	Restart(ctx context.Context, depID string, spec protocol.DeploySpec, containerID string) (string, bool, []string, error)
	Remove(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error)
	Logs(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error)
}

type Manager struct {
	Send   func(v any) error
	Runner Runner
}

func NewManager(send func(v any) error, r Runner) *Manager {
	if r == nil {
		r = NewComposite()
	}
	return &Manager{Send: send, Runner: r}
}

func (m *Manager) Handle(raw []byte) {
	var apply protocol.DeployApply
	if err := json.Unmarshal(raw, &apply); err != nil {
		return
	}
	if apply.Type != protocol.TypeDeployApply {
		return
	}
	go m.run(apply)
}

func (m *Manager) run(apply protocol.DeployApply) {
	ctx, cancel := context.WithTimeout(context.Background(), protocol.DeployApplyTimeout)
	defer cancel()

	res := protocol.DeployApplyResult{
		Type: protocol.TypeDeployApplyResult, RequestID: apply.RequestID,
	}
	var logs []string
	var err error
	key := apply.Spec.Name
	if apply.RemoveContainerID != "" {
		key = apply.RemoveContainerID
	}

	switch apply.Action {
	case protocol.DeployActionApply:
		res.ContainerID, res.HealthOK, logs, err = m.Runner.Apply(ctx, apply.DeploymentID, apply.RemoveContainerID, apply.Spec)
		res.Status = deployStatus(res.HealthOK, err)
	case protocol.DeployActionStop:
		if key == "" {
			key = apply.Spec.Name
		}
		res.ContainerID, logs, err = m.Runner.Stop(ctx, apply.Spec, key)
		res.Status = "stopped"
	case protocol.DeployActionRestart:
		res.ContainerID, res.HealthOK, logs, err = m.Runner.Restart(ctx, apply.DeploymentID, apply.Spec, apply.Spec.Name)
		res.Status = deployStatus(res.HealthOK, err)
	case protocol.DeployActionRemove:
		cid := apply.RemoveContainerID
		if cid == "" {
			cid = apply.Spec.Name
		}
		logs, err = m.Runner.Remove(ctx, apply.Spec, cid)
	case protocol.DeployActionLogs:
		logs, err = m.Runner.Logs(ctx, apply.Spec, apply.Spec.Name)
	default:
		err = fmt.Errorf("unknown deploy action %q", apply.Action)
	}

	if err != nil {
		res.OK = false
		res.Error = err.Error()
	} else {
		res.OK = true
	}
	res.LogLines = logs
	_ = m.Send(res)
}

func deployStatus(healthOK bool, err error) string {
	if err != nil {
		return "failed"
	}
	if healthOK {
		return "running"
	}
	return "unhealthy"
}

// Composite picks fake runner for knot-fake:* images, else docker when available.
type Composite struct {
	fake   *FakeRunner
	docker *DockerRunner
}

func NewComposite() *Composite {
	return &Composite{fake: NewFakeRunner(), docker: NewDockerRunner()}
}

func isFakeImage(image string) bool {
	return len(image) >= 9 && image[:9] == "knot-fake"
}

func (c *Composite) pick(spec protocol.DeploySpec) Runner {
	if isFakeImage(spec.Image) {
		return c.fake
	}
	if c.docker != nil && c.docker.Available() {
		return c.docker
	}
	return c.fake
}

func (c *Composite) Apply(ctx context.Context, depID, removeCID string, spec protocol.DeploySpec) (string, bool, []string, error) {
	return c.pick(spec).Apply(ctx, depID, removeCID, spec)
}

func (c *Composite) Stop(ctx context.Context, spec protocol.DeploySpec, containerID string) (string, []string, error) {
	return c.pick(spec).Stop(ctx, spec, containerID)
}

func (c *Composite) Restart(ctx context.Context, depID string, spec protocol.DeploySpec, containerID string) (string, bool, []string, error) {
	return c.pick(spec).Restart(ctx, depID, spec, containerID)
}

func (c *Composite) Remove(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	return c.pick(spec).Remove(ctx, spec, containerID)
}

func (c *Composite) Logs(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	return c.pick(spec).Logs(ctx, spec, containerID)
}
