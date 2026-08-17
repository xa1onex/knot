package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/builds"
	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrValidation    = errors.New("invalid release")
	ErrNotFound      = errors.New("release not found")
	ErrConflict      = errors.New("release already finished")
	ErrNothingToRoll = errors.New("no previous verified release to rollback")
	ErrUnhealthy     = errors.New("release health gate failed")
	ErrDeviceOffline = errors.New("device offline")
)

type Service struct {
	Store    *store.Store
	Deployer *deploy.Service
	Envs     *environments.Service
	Secrets  *secrets.Service
	Builds   *builds.Service
	Jobs     *jobs.Service
	Traffic  *traffic.Service
	Ops      *oplogs.Service
}

func New(st *store.Store, dep *deploy.Service, envs *environments.Service, sec *secrets.Service) *Service {
	return &Service{Store: st, Deployer: dep, Envs: envs, Secrets: sec}
}

type CreateRequest struct {
	UserID               string
	CreatedBy            string
	Service              string
	Image                string
	Environment          string
	EnvironmentID        string
	Project              string
	DeviceID             string
	Port                 int
	Bind                 string
	HealthPath           string
	HealthTimeoutSeconds int
	HealthRetries        int
	HealthExpectedStatus int
	Hostname             string
	EdgeDeviceID         string
	BuildID              string
	JobID                string
}

type DeployRequest struct {
	UserID   string
	ID       string
	DeviceID string
	Port     int
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.Release, error) {
	name, err := services.NormalizeDeployName(req.Service)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid service", ErrValidation)
	}
	image := strings.TrimSpace(req.Image)
	var buildTrace string
	if req.BuildID != "" && s.Builds != nil {
		b, berr := s.Builds.Get(ctx, req.UserID, req.BuildID)
		if berr != nil {
			return nil, fmt.Errorf("%w: build", ErrValidation)
		}
		if b.Status != protocol.BuildStatusCompleted || b.Image == "" {
			return nil, fmt.Errorf("%w: build is not completed", ErrValidation)
		}
		if image == "" {
			image = b.Image
		}
		buildTrace = b.TraceID
	}
	if buildTrace == "" && strings.TrimSpace(req.JobID) != "" && s.Jobs != nil {
		if j, jerr := s.Jobs.Get(ctx, req.UserID, req.JobID); jerr == nil {
			buildTrace = j.TraceID
		}
	}
	if image == "" || strings.ContainsAny(image, " \n\t") {
		return nil, fmt.Errorf("%w: image required", ErrValidation)
	}
	healthPath := strings.TrimSpace(req.HealthPath)
	if healthPath == "" {
		healthPath = "/health"
	}
	bind := strings.TrimSpace(req.Bind)
	if bind == "" {
		bind = "127.0.0.1"
	}
	timeout := req.HealthTimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}
	retries := req.HealthRetries
	if retries <= 0 {
		retries = 1
	}
	expect := req.HealthExpectedStatus
	if expect <= 0 {
		expect = 200
	}

	varsJSON, pinsJSON, envID, envName, configVer, err := s.snapshot(ctx, req)
	if err != nil {
		return nil, err
	}
	n, err := s.Store.MaxReleaseNumber(ctx, req.UserID, name)
	if err != nil {
		return nil, err
	}
	prevID := ""
	if cur, err := s.Store.GetCurrentRelease(ctx, req.UserID, name); err == nil && cur != nil {
		prevID = cur.ID
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}

	rel := &store.Release{
		ID: store.NewID(), UserID: req.UserID, Number: n + 1, Service: name, Image: image,
		EnvironmentID: envID, EnvironmentName: envName, ConfigVersion: configVer,
		VarsJSON: varsJSON, SecretPinsJSON: pinsJSON, Status: store.ReleaseStatusCreated,
		CreatedBy: req.CreatedBy, DeviceID: strings.TrimSpace(req.DeviceID), Port: req.Port, Bind: bind,
		HealthPath: healthPath, HealthTimeoutSeconds: timeout, HealthRetries: retries,
		HealthExpectedStatus: expect, Hostname: strings.TrimSpace(req.Hostname),
		EdgeDeviceID: strings.TrimSpace(req.EdgeDeviceID), BuildID: strings.TrimSpace(req.BuildID),
		JobID: strings.TrimSpace(req.JobID), PrevReleaseID: prevID,
		TraceID: oplogs.ResolveTrace(ctx, buildTrace),
	}
	if err := s.Store.CreateRelease(ctx, rel); err != nil {
		return nil, err
	}
	s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("created #%d image=%s env=%s config=%s", rel.Number, rel.Image, envName, configVer))
	return s.Store.GetRelease(ctx, req.UserID, rel.ID)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Release, error) {
	rel, err := s.Store.GetRelease(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rel, nil
}

func (s *Service) List(ctx context.Context, userID, service string) ([]store.Release, error) {
	if service != "" {
		name, err := services.NormalizeDeployName(service)
		if err == nil {
			service = name
		}
	}
	return s.Store.ListReleases(ctx, userID, service)
}

func (s *Service) Current(ctx context.Context, userID, service string) (*store.Release, error) {
	name, err := services.NormalizeDeployName(service)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid service", ErrValidation)
	}
	rel, err := s.Store.GetCurrentRelease(ctx, userID, name)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rel, nil
}

func (s *Service) Deploy(ctx context.Context, req DeployRequest) (*store.Release, error) {
	rel, err := s.Get(ctx, req.UserID, req.ID)
	if err != nil {
		return nil, err
	}
	if rel.Status != store.ReleaseStatusCreated {
		return nil, fmt.Errorf("%w: deploy from created only", ErrConflict)
	}
	if s.Deployer == nil {
		return nil, fmt.Errorf("%w: deploy unavailable", ErrValidation)
	}
	if req.DeviceID != "" {
		rel.DeviceID = req.DeviceID
	}
	if req.Port > 0 {
		rel.Port = req.Port
	}
	if rel.DeviceID == "" || rel.Port < 1 {
		return nil, fmt.Errorf("%w: device_id and port required to deploy", ErrValidation)
	}

	rel.Status = store.ReleaseStatusDeploying
	rel.Error = ""
	_ = s.Store.UpdateRelease(ctx, rel)
	s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("deploying candidate image=%s", rel.Image))

	keepPrev := false
	if prev, err := s.Store.GetCurrentRelease(ctx, req.UserID, rel.Service); err == nil && prev != nil {
		if prev.Port > 0 && prev.Port != rel.Port {
			keepPrev = true
			s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("blue/green beside #%d port %d", prev.Number, prev.Port))
		}
	}

	env := deploy.ParseEnvJSON(rel.VarsJSON)
	dep, err := s.Deployer.Create(ctx, deploy.CreateRequest{
		UserID: req.UserID, DeviceID: rel.DeviceID, Name: rel.Service, Image: rel.Image,
		Port: rel.Port, Bind: rel.Bind, HealthPath: rel.HealthPath, Env: env,
		Hostname: rel.Hostname, EdgeDeviceID: rel.EdgeDeviceID,
		EnvironmentID: rel.EnvironmentID, Snapshot: true, SecretPinsJSON: rel.SecretPinsJSON,
		SkipAutoRollback: true, KeepPrevious: keepPrev, ReleaseID: rel.ID,
	})
	if dep != nil {
		rel.DeploymentID = dep.ID
	}
	if err != nil && !errors.Is(err, deploy.ErrUnhealthy) {
		rel.Status = store.ReleaseStatusFailed
		rel.Error = deploy.SanitizeLogLine(err.Error())
		_ = s.Store.UpdateRelease(ctx, rel)
		s.log(ctx, rel.ID, "stderr", "release", rel.Error)
		if errors.Is(err, deploy.ErrDeviceOffline) {
			return rel, ErrDeviceOffline
		}
		return rel, err
	}

	rel.Status = store.ReleaseStatusTesting
	_ = s.Store.UpdateRelease(ctx, rel)
	s.log(ctx, rel.ID, "stdout", "release", "health gate")

	ok := err == nil && dep != nil && dep.HealthOK && dep.Image == rel.Image
	if !ok {
		ok = s.retryHealth(ctx, rel, dep)
	}
	if !ok {
		return s.failHealth(ctx, rel, dep)
	}

	rel.Status = store.ReleaseStatusActive
	rel.Current = true
	rel.Error = ""
	_ = s.Store.ClearCurrentRelease(ctx, rel.UserID, rel.Service)
	_ = s.Store.UpdateRelease(ctx, rel)
	s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("active #%d image=%s", rel.Number, rel.Image))
	if s.Traffic != nil {
		s.Traffic.OnCandidateReady(ctx, rel.UserID, rel.Service, rel.ID)
	}
	return s.Store.GetRelease(ctx, req.UserID, rel.ID)
}

func (s *Service) Rollback(ctx context.Context, userID, id string) (*store.Release, error) {
	rel, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !rel.Current {
		return nil, fmt.Errorf("%w: only the current release can be rolled back", ErrConflict)
	}
	if rel.PrevReleaseID == "" {
		return nil, ErrNothingToRoll
	}
	prev, err := s.Get(ctx, userID, rel.PrevReleaseID)
	if err != nil {
		return nil, err
	}
	s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("rollback to #%d image=%s", prev.Number, prev.Image))
	restored, err := s.restore(ctx, prev)
	if err != nil {
		rel.Error = deploy.SanitizeLogLine(err.Error())
		_ = s.Store.UpdateRelease(ctx, rel)
		return rel, err
	}
	rel.Status = store.ReleaseStatusRolledBack
	rel.Current = false
	rel.Error = ""
	_ = s.Store.UpdateRelease(ctx, rel)
	s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("rolled_back restored #%d", restored.Number))
	return s.Store.GetRelease(ctx, userID, rel.ID)
}

func (s *Service) Cancel(ctx context.Context, userID, id string) (*store.Release, error) {
	rel, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if rel.Status != store.ReleaseStatusCreated {
		return nil, fmt.Errorf("%w: only created releases can be cancelled", ErrConflict)
	}
	rel.Status = store.ReleaseStatusCancelled
	_ = s.Store.UpdateRelease(ctx, rel)
	s.log(ctx, rel.ID, "stdout", "release", "cancelled")
	return rel, nil
}

func (s *Service) Logs(ctx context.Context, userID, id string, limit int) ([]store.ReleaseLog, error) {
	rel, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	out, err := s.Store.ListReleaseLogs(ctx, rel.ID, limit)
	if err != nil {
		return nil, err
	}
	remain := limit - len(out)
	if remain <= 0 {
		remain = 50
	}
	if rel.DeploymentID != "" && s.Deployer != nil {
		if dlogs, err := s.Deployer.Logs(ctx, userID, rel.DeploymentID, remain); err == nil {
			for i := len(dlogs) - 1; i >= 0; i-- {
				l := dlogs[i]
				out = append(out, store.ReleaseLog{
					ID: l.ID, ReleaseID: rel.ID, Stream: l.Stream, Source: "deploy",
					Message: l.Message, CreatedAt: l.CreatedAt,
				})
			}
		}
	}
	if rel.BuildID != "" && s.Builds != nil {
		if blogs, err := s.Builds.Logs(ctx, userID, rel.BuildID, remain); err == nil {
			for _, l := range blogs {
				out = append(out, store.ReleaseLog{
					ID: l.ID, ReleaseID: rel.ID, Stream: l.Stream, Source: "build",
					Message: l.Message, CreatedAt: l.CreatedAt,
				})
			}
		}
	}
	if rel.JobID != "" && s.Jobs != nil {
		if jlogs, err := s.Jobs.Logs(ctx, userID, rel.JobID, remain); err == nil {
			for _, l := range jlogs {
				out = append(out, store.ReleaseLog{
					ID: l.ID, ReleaseID: rel.ID, Stream: l.Stream, Source: "job",
					Message: l.Message, CreatedAt: l.CreatedAt,
				})
			}
		}
	}
	return out, nil
}

func (s *Service) retryHealth(ctx context.Context, rel *store.Release, dep *store.Deployment) bool {
	if dep == nil || s.Deployer == nil {
		return false
	}
	tries := rel.HealthRetries
	if tries < 1 {
		tries = 1
	}
	wait := time.Duration(rel.HealthTimeoutSeconds) * time.Second
	if wait <= 0 {
		wait = 2 * time.Second
	}
	for i := 0; i < tries; i++ {
		if dep.HealthOK && dep.Image == rel.Image && (rel.HealthExpectedStatus == 200 || rel.HealthExpectedStatus == 0) {
			return true
		}
		s.log(ctx, rel.ID, "stdout", "release", fmt.Sprintf("health retry %d/%d", i+1, tries))
		if i+1 >= tries {
			break
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait / time.Duration(tries)):
		}
		restarted, err := s.Deployer.Restart(ctx, rel.UserID, dep.ID)
		if err != nil || restarted == nil {
			continue
		}
		dep.HealthOK = restarted.HealthOK
		dep.Image = restarted.Image
	}
	return dep.HealthOK && dep.Image == rel.Image
}

func (s *Service) failHealth(ctx context.Context, rel *store.Release, dep *store.Deployment) (*store.Release, error) {
	s.log(ctx, rel.ID, "stderr", "release", "health failed; restoring previous release")
	if dep != nil && s.Deployer != nil {
		_, _ = s.Deployer.Stop(ctx, rel.UserID, dep.ID)
	}
	sideBySide := false
	if rel.PrevReleaseID != "" {
		if prev, err := s.Get(ctx, rel.UserID, rel.PrevReleaseID); err == nil && prev.Port > 0 && prev.Port != rel.Port {
			sideBySide = true
			s.log(ctx, rel.ID, "stdout", "release", "candidate disabled; previous origin still running")
		}
	}
	if !sideBySide && rel.PrevReleaseID != "" {
		if prev, err := s.Get(ctx, rel.UserID, rel.PrevReleaseID); err == nil {
			if _, rerr := s.restore(ctx, prev); rerr != nil {
				rel.Status = store.ReleaseStatusFailed
				rel.Error = deploy.SanitizeLogLine("health failed; restore: " + rerr.Error())
				rel.Current = false
				_ = s.Store.UpdateRelease(ctx, rel)
				return rel, fmt.Errorf("%w: %s", ErrUnhealthy, rel.Error)
			}
		}
	}
	rel.Status = store.ReleaseStatusFailed
	rel.Current = false
	rel.Error = ErrUnhealthy.Error()
	if dep != nil && dep.Error != "" {
		rel.Error = deploy.SanitizeLogLine(dep.Error)
	}
	_ = s.Store.UpdateRelease(ctx, rel)
	return s.Store.GetRelease(ctx, rel.UserID, rel.ID)
}

func (s *Service) restore(ctx context.Context, prev *store.Release) (*store.Release, error) {
	if s.Deployer == nil {
		return nil, fmt.Errorf("%w: deploy unavailable", ErrValidation)
	}
	if prev.DeviceID == "" || prev.Port < 1 {
		return nil, fmt.Errorf("%w: previous release missing device/port", ErrValidation)
	}
	env := deploy.ParseEnvJSON(prev.VarsJSON)
	dep, err := s.Deployer.Create(ctx, deploy.CreateRequest{
		UserID: prev.UserID, DeviceID: prev.DeviceID, Name: prev.Service, Image: prev.Image,
		Port: prev.Port, Bind: prev.Bind, HealthPath: prev.HealthPath, Env: env,
		EnvironmentID: prev.EnvironmentID, Snapshot: true, SecretPinsJSON: prev.SecretPinsJSON,
		SkipAutoRollback: true, ReleaseID: prev.ID,
	})
	if err != nil {
		return nil, err
	}
	if dep != nil {
		prev.DeploymentID = dep.ID
	}
	_ = s.Store.ClearCurrentRelease(ctx, prev.UserID, prev.Service)
	prev.Status = store.ReleaseStatusActive
	prev.Current = true
	prev.Error = ""
	_ = s.Store.UpdateRelease(ctx, prev)
	s.log(ctx, prev.ID, "stdout", "release", fmt.Sprintf("restored #%d image=%s", prev.Number, prev.Image))
	return prev, nil
}

func (s *Service) snapshot(ctx context.Context, req CreateRequest) (varsJSON, pinsJSON, envID, envName, configVer string, err error) {
	vars := map[string]string{}
	pins := map[string]deploy.SecretPin{}
	if req.EnvironmentID == "" && req.Environment == "" {
		b, _ := json.Marshal(vars)
		return string(b), "{}", "", "", "", nil
	}
	if s.Envs == nil {
		return "", "", "", "", "", fmt.Errorf("%w: environments unavailable", ErrValidation)
	}
	var e *store.Environment
	if req.EnvironmentID != "" {
		st, lerr := s.Store.GetEnvironment(ctx, req.UserID, req.EnvironmentID)
		if lerr != nil {
			if store.IsNotFound(lerr) {
				return "", "", "", "", "", environments.ErrNotFound
			}
			return "", "", "", "", "", lerr
		}
		e = st
	} else {
		project := req.Project
		if project == "" {
			project = req.Service
		}
		st, lerr := s.Envs.Lookup(ctx, req.UserID, project, req.Environment)
		if lerr != nil {
			return "", "", "", "", "", lerr
		}
		e = st
	}
	envID = e.ID
	envName = e.Name
	configVer = e.UpdatedAt.UTC().Format(time.RFC3339Nano)
	vars = deploy.ParseEnvJSON(e.VarsJSON)
	refs := deploy.ParseEnvJSON(e.SecretsJSON)
	for key, secretID := range refs {
		if s.Secrets == nil {
			return "", "", "", "", "", fmt.Errorf("%w: secrets unavailable", ErrValidation)
		}
		meta, gerr := s.Secrets.Get(ctx, req.UserID, secretID)
		if gerr != nil {
			return "", "", "", "", "", gerr
		}
		pins[key] = deploy.SecretPin{SecretID: meta.ID, Name: meta.Name, Version: meta.Version}
	}
	vb, _ := json.Marshal(vars)
	if len(vars) == 0 {
		vb = []byte("{}")
	}
	pb, _ := json.Marshal(pins)
	if len(pins) == 0 {
		pb = []byte("{}")
	}
	return string(vb), string(pb), envID, envName, configVer, nil
}

func (s *Service) log(ctx context.Context, releaseID, stream, source, message string) {
	msg := deploy.SanitizeLogLine(message)
	_ = s.Store.AppendReleaseLog(ctx, releaseID, stream, source, msg)
	rel, err := s.Store.GetReleaseByID(ctx, releaseID)
	if err != nil {
		return
	}
	src := oplogs.SourceRelease
	if source != "" && oplogs.ValidSource(source) {
		src = source
	}
	s.Ops.Emit(ctx, oplogs.Event{
		UserID: rel.UserID, Source: src, Level: oplogs.LevelFromStream(stream),
		Message: msg, TraceID: rel.TraceID, DeviceID: rel.DeviceID, Service: rel.Service,
		ReleaseID: rel.ID, BuildID: rel.BuildID, JobID: rel.JobID, DeploymentID: rel.DeploymentID,
	})
}
