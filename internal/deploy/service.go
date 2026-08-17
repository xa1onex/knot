package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrValidation    = errors.New("invalid deployment")
	ErrNotFound      = errors.New("deployment not found")
	ErrDevice        = errors.New("device not found")
	ErrDeviceOffline = errors.New("device offline")
	ErrAgent         = errors.New("agent deploy failed")
	ErrTimeout       = errors.New("deploy timeout")
	ErrUnhealthy     = errors.New("deployment unhealthy")
	ErrNothingActive = errors.New("no active deployment")
	ErrNothingToRoll = errors.New("no previous revision to rollback")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Service struct {
	Store     *store.Store
	Sender    Sender
	Services  *services.Service
	Edge      *edge.Proxy
	Secrets   *secrets.Service
	Envs      *environments.Service
	Ops       *oplogs.Service
	Timeout   time.Duration
	HealthTry time.Duration

	mu      sync.Mutex
	pending map[string]chan protocol.DeployApplyResult
}

func New(st *store.Store, sender Sender, svcReg *services.Service, edgeProxy *edge.Proxy) *Service {
	return &Service{
		Store:     st,
		Sender:    sender,
		Services:  svcReg,
		Edge:      edgeProxy,
		Timeout:   protocol.DeployApplyTimeout,
		HealthTry: 15 * time.Second,
		pending:   make(map[string]chan protocol.DeployApplyResult),
	}
}

type CreateRequest struct {
	UserID           string
	DeviceID         string
	Name             string
	Image            string
	Runtime          string
	Port             int
	Bind             string
	HealthPath       string
	Env              map[string]string
	Kind             string
	Hostname         string
	EdgeDeviceID     string
	Environment      string
	EnvironmentID    string
	Project          string
	SkipAutoRollback bool
	KeepPrevious     bool
	Snapshot         bool
	SecretPinsJSON   string
	ReleaseID        string
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.Deployment, error) {
	req = normalizeCreate(req)
	if err := s.validateCreate(ctx, req); err != nil {
		return nil, err
	}
	if !s.Sender.IsOnline(req.DeviceID) {
		return nil, ErrDeviceOffline
	}
	name, err := services.NormalizeDeployName(req.Name)
	if err != nil {
		return nil, err
	}
	prev, _ := s.Store.GetActiveDeploymentByName(ctx, req.UserID, req.DeviceID, name)
	rev, err := s.Store.MaxDeploymentRevision(ctx, req.UserID, req.DeviceID, name)
	if err != nil {
		return nil, err
	}
	rev++

	prevID := ""
	removeCID := ""
	if prev != nil {
		prevID = prev.ID
		if !req.KeepPrevious {
			removeCID = prev.ContainerID
		}
	}

	envVars, pinsJSON, envID, err := s.materializeEnv(ctx, req)
	if err != nil {
		return nil, err
	}

	dep := &store.Deployment{
		UserID:           req.UserID,
		DeviceID:         req.DeviceID,
		Name:             name,
		Runtime:          req.Runtime,
		Image:            req.Image,
		Port:             req.Port,
		Bind:             req.Bind,
		EnvJSON:          envJSON(publicVars(envVars, pinsJSON)),
		HealthPath:       req.HealthPath,
		Revision:         rev,
		Status:           store.DeployStatusStarting,
		PrevDeploymentID: prevID,
		EnvironmentID:    envID,
		SecretPinsJSON:   pinsJSON,
		ReleaseID:        req.ReleaseID,
	}
	if req.ReleaseID != "" {
		if rel, err := s.Store.GetRelease(ctx, req.UserID, req.ReleaseID); err == nil {
			dep.TraceID = rel.TraceID
		}
	}
	if dep.TraceID == "" {
		dep.TraceID = oplogs.ResolveTrace(ctx, "")
	}
	if err := s.Store.CreateDeployment(ctx, dep); err != nil {
		return nil, err
	}
	s.log(ctx, dep.ID, "stdout", fmt.Sprintf("deploy revision %d starting image=%s", rev, req.Image))

	res, err := s.callAgent(ctx, req.DeviceID, protocol.DeployApply{
		Action:            protocol.DeployActionApply,
		DeploymentID:      dep.ID,
		RemoveContainerID: removeCID,
		Spec: protocol.DeploySpec{
			Name: name, Runtime: dep.Runtime, Image: req.Image, Port: req.Port,
			Bind: req.Bind, HealthPath: req.HealthPath, Env: envVars,
			Limits: defaultLimits(),
		},
	})
	if err != nil {
		dep.Status = store.DeployStatusFailed
		dep.Error = err.Error()
		_ = s.Store.UpdateDeployment(ctx, dep)
		s.cleanupContainer(ctx, req.DeviceID, dep.ID, res.ContainerID, name, envVars, req.Port, req.Bind, req.HealthPath, req.Runtime, req.Image)
		return dep, err
	}

	dep.ContainerID = res.ContainerID
	dep.HealthOK = res.HealthOK
	for _, line := range res.LogLines {
		s.log(ctx, dep.ID, "stdout", SanitizeLogLine(line))
	}

	if !res.HealthOK {
		dep.Status = store.DeployStatusUnhealthy
		dep.Error = ErrUnhealthy.Error()
		_ = s.Store.UpdateDeployment(ctx, dep)
		if req.SkipAutoRollback {
			// Leave the candidate running so the release health gate can retry, then disable it.
			return dep, ErrUnhealthy
		}
		s.cleanupContainer(ctx, req.DeviceID, dep.ID, res.ContainerID, name, envVars, req.Port, req.Bind, req.HealthPath, req.Runtime, req.Image)
		if prev != nil {
			rolled, rollErr := s.rollbackTo(ctx, req.UserID, prev)
			if rollErr == nil {
				s.log(ctx, rolled.ID, "stdout", "auto-rollback after unhealthy deploy")
				return rolled, nil
			}
			return dep, fmt.Errorf("%w: auto-rollback failed: %v", ErrUnhealthy, rollErr)
		}
		return dep, ErrUnhealthy
	}

	if req.KeepPrevious {
		dep.Status = store.DeployStatusRunning
		dep.HealthOK = true
		dep.Active = false
		dep.Error = ""
		if prev != nil {
			dep.ServiceID = prev.ServiceID
		}
		_ = s.Store.UpdateDeployment(ctx, dep)
		s.log(ctx, dep.ID, "stdout", "candidate running beside previous (traffic unchanged)")
		return s.Store.GetDeployment(ctx, req.UserID, dep.ID)
	}

	if err := s.activate(ctx, dep, prev); err != nil {
		dep.Status = store.DeployStatusFailed
		dep.Error = err.Error()
		_ = s.Store.UpdateDeployment(ctx, dep)
		return dep, err
	}

	if req.Hostname != "" && s.Edge != nil {
		svc, err := s.Store.GetService(ctx, req.UserID, dep.ServiceID)
		if err == nil && svc != nil {
			_, _ = s.Edge.CreateRoute(ctx, edge.CreateRouteRequest{
				UserID: req.UserID, Hostname: req.Hostname, ServiceID: svc.ID, EdgeDeviceID: req.EdgeDeviceID,
			})
		}
	}

	return s.Store.GetDeployment(ctx, req.UserID, dep.ID)
}

func (s *Service) Stop(ctx context.Context, userID, id string) (*store.Deployment, error) {
	dep, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !s.Sender.IsOnline(dep.DeviceID) {
		return nil, ErrDeviceOffline
	}
	env, err := s.agentEnv(ctx, dep)
	if err != nil {
		return nil, err
	}
	res, err := s.callAgent(ctx, dep.DeviceID, protocol.DeployApply{
		Action: protocol.DeployActionStop, DeploymentID: dep.ID,
		Spec: specFromDep(dep, env),
	})
	if err != nil {
		return nil, err
	}
	dep.Status = store.DeployStatusStopped
	dep.HealthOK = false
	dep.Active = false
	dep.ContainerID = res.ContainerID
	_ = s.Store.UpdateDeployment(ctx, dep)
	s.log(ctx, dep.ID, "stdout", "stopped")
	return dep, nil
}

func (s *Service) Restart(ctx context.Context, userID, id string) (*store.Deployment, error) {
	dep, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !s.Sender.IsOnline(dep.DeviceID) {
		return nil, ErrDeviceOffline
	}
	env, err := s.agentEnv(ctx, dep)
	if err != nil {
		return nil, err
	}
	res, err := s.callAgent(ctx, dep.DeviceID, protocol.DeployApply{
		Action: protocol.DeployActionRestart, DeploymentID: dep.ID,
		Spec: specFromDep(dep, env),
	})
	if err != nil {
		dep.Status = store.DeployStatusFailed
		dep.Error = err.Error()
		_ = s.Store.UpdateDeployment(ctx, dep)
		return dep, err
	}
	dep.ContainerID = res.ContainerID
	dep.HealthOK = res.HealthOK
	if res.HealthOK {
		active, _ := s.Store.GetActiveDeploymentByName(ctx, userID, dep.DeviceID, dep.Name)
		if active != nil && active.ID != dep.ID {
			if err := s.activate(ctx, dep, active); err != nil {
				return dep, err
			}
			return dep, nil
		}
		dep.Status = store.DeployStatusRunning
		dep.Error = ""
		if active == nil {
			dep.Active = true
		}
	} else {
		dep.Status = store.DeployStatusUnhealthy
		dep.Error = ErrUnhealthy.Error()
	}
	_ = s.Store.UpdateDeployment(ctx, dep)
	return dep, nil
}

func (s *Service) Rollback(ctx context.Context, userID, id string) (*store.Deployment, error) {
	dep, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if dep.PrevDeploymentID == "" {
		return nil, ErrNothingToRoll
	}
	prev, err := s.Store.GetDeployment(ctx, userID, dep.PrevDeploymentID)
	if err != nil {
		return nil, err
	}
	return s.rollbackTo(ctx, userID, prev)
}

func (s *Service) rollbackTo(ctx context.Context, userID string, prev *store.Deployment) (*store.Deployment, error) {
	if !s.Sender.IsOnline(prev.DeviceID) {
		return nil, ErrDeviceOffline
	}
	active, _ := s.Store.GetActiveDeploymentByName(ctx, userID, prev.DeviceID, prev.Name)
	removeCID := ""
	if active != nil {
		removeCID = active.ContainerID
	}
	env, err := s.agentEnv(ctx, prev)
	if err != nil {
		return nil, err
	}
	rev, _ := s.Store.MaxDeploymentRevision(ctx, userID, prev.DeviceID, prev.Name)
	rev++
	dep := &store.Deployment{
		UserID:           userID,
		DeviceID:         prev.DeviceID,
		Name:             prev.Name,
		Runtime:          prev.Runtime,
		Image:            prev.Image,
		Port:             prev.Port,
		Bind:             prev.Bind,
		EnvJSON:          prev.EnvJSON,
		HealthPath:       prev.HealthPath,
		Revision:         rev,
		Status:           store.DeployStatusStarting,
		PrevDeploymentID: prev.ID,
		EnvironmentID:    prev.EnvironmentID,
		SecretPinsJSON:   prev.SecretPinsJSON,
	}
	if err := s.Store.CreateDeployment(ctx, dep); err != nil {
		return nil, err
	}
	res, err := s.callAgent(ctx, prev.DeviceID, protocol.DeployApply{
		Action: protocol.DeployActionApply, DeploymentID: dep.ID, RemoveContainerID: removeCID,
		Spec: specFromDep(prev, env),
	})
	if err != nil || !res.HealthOK {
		dep.Status = store.DeployStatusFailed
		if err != nil {
			dep.Error = err.Error()
		} else {
			dep.Error = ErrUnhealthy.Error()
		}
		_ = s.Store.UpdateDeployment(ctx, dep)
		if err != nil {
			return dep, err
		}
		return dep, ErrUnhealthy
	}
	dep.ContainerID = res.ContainerID
	if err := s.activate(ctx, dep, active); err != nil {
		return dep, err
	}
	s.log(ctx, dep.ID, "stdout", fmt.Sprintf("rolled back to image=%s rev=%d", prev.Image, prev.Revision))
	return s.Store.GetDeployment(ctx, userID, dep.ID)
}

func (s *Service) Logs(ctx context.Context, userID, id string, limit int) ([]store.DeploymentLog, error) {
	dep, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if s.Sender.IsOnline(dep.DeviceID) {
		if env, envErr := s.agentEnv(ctx, dep); envErr == nil {
			res, err := s.callAgent(ctx, dep.DeviceID, protocol.DeployApply{
				Action: protocol.DeployActionLogs, DeploymentID: dep.ID,
				Spec: specFromDep(dep, env),
			})
			if err == nil {
				for _, line := range res.LogLines {
					s.log(ctx, dep.ID, "stdout", line)
				}
			}
		}
	}
	return s.Store.ListDeploymentLogs(ctx, dep.ID, limit)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Deployment, error) {
	return s.get(ctx, userID, id)
}

func (s *Service) List(ctx context.Context, userID, deviceID, name string) ([]store.Deployment, error) {
	list, err := s.Store.ListDeployments(ctx, userID, deviceID, name)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []store.Deployment{}
	}
	return list, nil
}

func (s *Service) HandleAgentMessage(_ context.Context, _ string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeDeployApplyResult:
		var res protocol.DeployApplyResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		s.mu.Lock()
		ch := s.pending[res.RequestID]
		s.mu.Unlock()
		if ch == nil {
			return nil
		}
		select {
		case ch <- res:
		default:
		}
	case protocol.TypeDeployLogLine:
		var msg protocol.DeployLogLine
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		s.log(context.Background(), msg.DeploymentID, msg.Stream, msg.Message)
	}
	return nil
}

func (s *Service) OnDeviceDisconnect(deviceID string) {
	ctx := context.Background()
	list, err := s.Store.ListActiveDeploymentsByDevice(ctx, deviceID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for i := range list {
		d := &list[i]
		d.HealthOK = false
		d.Status = store.DeployStatusStopped
		d.Error = "agent disconnected"
		d.UpdatedAt = now
		_ = s.Store.UpdateDeployment(ctx, d)
	}
}

func (s *Service) activate(ctx context.Context, dep *store.Deployment, prev *store.Deployment) error {
	_ = s.Store.DeactivateDeployments(ctx, dep.UserID, dep.DeviceID, dep.Name)
	dep.Active = true
	dep.Status = store.DeployStatusRunning
	dep.HealthOK = true
	dep.Error = ""

	svcID, err := s.syncServiceRegistry(ctx, dep)
	if err != nil {
		return err
	}
	dep.ServiceID = svcID
	if err := s.Store.UpdateDeployment(ctx, dep); err != nil {
		return err
	}
	if prev != nil && prev.Active {
		prev.Active = false
		_ = s.Store.UpdateDeployment(ctx, prev)
	}
	return nil
}

func (s *Service) syncServiceRegistry(ctx context.Context, dep *store.Deployment) (string, error) {
	if s.Services == nil {
		return "", nil
	}
	if existing, err := s.Store.GetServiceByName(ctx, dep.UserID, dep.DeviceID, dep.Name); err == nil && existing != nil {
		port := dep.Port
		bind := dep.Bind
		existing.Port = port
		existing.Bind = bind
		existing.Status = store.ServiceStatusRegistered
		if err := s.Store.UpdateService(ctx, existing); err != nil {
			return "", err
		}
		return existing.ID, nil
	}
	svc, err := s.Services.Register(ctx, services.RegisterRequest{
		UserID: dep.UserID, DeviceID: dep.DeviceID, Name: dep.Name,
		Kind: "web", Protocol: "http", Port: dep.Port, Bind: dep.Bind,
	})
	if err != nil {
		return "", err
	}
	return svc.ID, nil
}

func (s *Service) callAgent(ctx context.Context, deviceID string, apply protocol.DeployApply) (protocol.DeployApplyResult, error) {
	if !s.Sender.IsOnline(deviceID) {
		return protocol.DeployApplyResult{}, ErrDeviceOffline
	}
	reqID := store.NewID()
	apply.Type = protocol.TypeDeployApply
	apply.RequestID = reqID
	ch := make(chan protocol.DeployApplyResult, 1)
	s.mu.Lock()
	s.pending[reqID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, reqID)
		s.mu.Unlock()
	}()
	if err := s.Sender.SendJSON(deviceID, apply); err != nil {
		return protocol.DeployApplyResult{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = protocol.DeployApplyTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return protocol.DeployApplyResult{}, ctx.Err()
	case <-timer.C:
		return protocol.DeployApplyResult{}, ErrTimeout
	case res := <-ch:
		if !res.OK {
			msg := res.Error
			if msg == "" {
				msg = "agent rejected deploy"
			}
			return res, fmt.Errorf("%w: %s", ErrAgent, msg)
		}
		return res, nil
	}
}

func (s *Service) cleanupContainer(ctx context.Context, deviceID, depID, containerID, name string, env map[string]string, port int, bind, healthPath, runtime, image string) {
	if containerID == "" {
		return
	}
	_, _ = s.callAgent(ctx, deviceID, protocol.DeployApply{
		Action: protocol.DeployActionRemove, DeploymentID: depID, RemoveContainerID: containerID,
		Spec: protocol.DeploySpec{Name: name, Runtime: runtime, Image: image, Port: port, Bind: bind, HealthPath: healthPath, Env: env},
	})
}

func (s *Service) get(ctx context.Context, userID, id string) (*store.Deployment, error) {
	dep, err := s.Store.GetDeployment(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return dep, nil
}

func (s *Service) log(ctx context.Context, depID, stream, msg string) {
	line := SanitizeLogLine(msg)
	_ = s.Store.AppendDeploymentLog(ctx, depID, stream, line)
	dep, err := s.Store.GetDeploymentByID(ctx, depID)
	if err != nil {
		return
	}
	s.Ops.Emit(ctx, oplogs.Event{
		UserID: dep.UserID, Source: oplogs.SourceDeploy, Level: oplogs.LevelFromStream(stream),
		Message: line, TraceID: dep.TraceID, DeviceID: dep.DeviceID, ServiceID: dep.ServiceID,
		Service: dep.Name, ReleaseID: dep.ReleaseID, DeploymentID: dep.ID,
	})
}

func (s *Service) validateCreate(ctx context.Context, req CreateRequest) error {
	if req.DeviceID == "" || req.Name == "" || req.Image == "" {
		return fmt.Errorf("%w: device_id, name, and image required", ErrValidation)
	}
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("%w: port must be 1–65535", ErrValidation)
	}
	if req.Runtime != "docker" {
		return fmt.Errorf("%w: only runtime=docker supported in 7.4", ErrValidation)
	}
	if _, err := s.Store.GetDevice(ctx, req.UserID, req.DeviceID); err != nil {
		return ErrDevice
	}
	return nil
}

func normalizeCreate(req CreateRequest) CreateRequest {
	if req.Runtime == "" {
		req.Runtime = "docker"
	}
	if req.Bind == "" {
		req.Bind = "127.0.0.1"
	}
	if req.HealthPath == "" {
		req.HealthPath = "/health"
	}
	return req
}

func specFromDep(dep *store.Deployment, env map[string]string) protocol.DeploySpec {
	return protocol.DeploySpec{
		Name: dep.Name, Runtime: dep.Runtime, Image: dep.Image,
		Port: dep.Port, Bind: dep.Bind, HealthPath: dep.HealthPath, Env: env,
		Limits: defaultLimits(),
	}
}

func defaultLimits() protocol.DeployLimits {
	return protocol.DeployLimits{
		CPUs: protocol.DefaultDeployCPUs, MemoryMB: protocol.DefaultDeployMemoryMB,
		Pids: protocol.DefaultDeployPids, ReadOnly: false,
	}
}

type SecretPin struct {
	SecretID string `json:"secret_id"`
	Name     string `json:"name"`
	Version  int    `json:"version"`
}

func ParseSecretPins(raw string) map[string]SecretPin {
	if raw == "" || raw == "{}" {
		return map[string]SecretPin{}
	}
	out := map[string]SecretPin{}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]SecretPin{}
	}
	return out
}

func (s *Service) materializeEnv(ctx context.Context, req CreateRequest) (env map[string]string, pinsJSON, envID string, err error) {
	env = map[string]string{}
	for k, v := range req.Env {
		env[k] = v
	}
	if req.Snapshot {
		pins := ParseSecretPins(req.SecretPinsJSON)
		for key, pin := range pins {
			if s.Secrets == nil {
				return nil, "", "", fmt.Errorf("%w: secrets unavailable", ErrValidation)
			}
			val, rerr := s.Secrets.Reveal(ctx, req.UserID, pin.SecretID, pin.Version)
			if rerr != nil {
				return nil, "", "", rerr
			}
			env[key] = val
		}
		pinsJSON = req.SecretPinsJSON
		if pinsJSON == "" {
			pinsJSON = "{}"
		}
		return env, pinsJSON, req.EnvironmentID, nil
	}
	pins := map[string]SecretPin{}
	var e *store.Environment
	if req.EnvironmentID != "" && s.Envs != nil {
		rec, gerr := s.Envs.Get(ctx, req.UserID, req.EnvironmentID)
		if gerr != nil {
			return nil, "", "", gerr
		}
		st, lerr := s.Store.GetEnvironment(ctx, req.UserID, rec.ID)
		if lerr != nil {
			return nil, "", "", lerr
		}
		e = st
	} else if req.Environment != "" {
		if s.Envs == nil {
			return nil, "", "", fmt.Errorf("%w: environments unavailable", ErrValidation)
		}
		project := req.Project
		if project == "" {
			project = req.Name
		}
		st, lerr := s.Envs.Lookup(ctx, req.UserID, project, req.Environment)
		if lerr != nil {
			return nil, "", "", lerr
		}
		e = st
	}
	if e != nil {
		envID = e.ID
		for k, v := range parseEnvJSON(e.VarsJSON) {
			env[k] = v
		}
		refs := parseEnvJSON(e.SecretsJSON)
		for key, secretID := range refs {
			if s.Secrets == nil {
				return nil, "", "", fmt.Errorf("%w: secrets unavailable", ErrValidation)
			}
			meta, gerr := s.Secrets.Get(ctx, req.UserID, secretID)
			if gerr != nil {
				return nil, "", "", gerr
			}
			val, rerr := s.Secrets.Reveal(ctx, req.UserID, meta.ID, meta.Version)
			if rerr != nil {
				return nil, "", "", rerr
			}
			env[key] = val
			pins[key] = SecretPin{SecretID: meta.ID, Name: meta.Name, Version: meta.Version}
		}
	}
	if len(pins) == 0 {
		return env, "{}", envID, nil
	}
	b, _ := json.Marshal(pins)
	return env, string(b), envID, nil
}

func (s *Service) agentEnv(ctx context.Context, dep *store.Deployment) (map[string]string, error) {
	env := map[string]string{}
	for k, v := range parseEnvJSON(dep.EnvJSON) {
		env[k] = v
	}
	for key, pin := range ParseSecretPins(dep.SecretPinsJSON) {
		if s.Secrets == nil {
			return nil, fmt.Errorf("%w: secrets unavailable", ErrValidation)
		}
		val, err := s.Secrets.Reveal(ctx, dep.UserID, pin.SecretID, pin.Version)
		if err != nil {
			return nil, err
		}
		env[key] = val
	}
	return env, nil
}

func publicVars(env map[string]string, pinsJSON string) map[string]string {
	pins := ParseSecretPins(pinsJSON)
	out := map[string]string{}
	for k, v := range env {
		if _, secret := pins[k]; secret {
			continue
		}
		out[k] = v
	}
	return out
}
