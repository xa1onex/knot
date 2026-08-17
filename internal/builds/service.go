package builds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrValidation    = errors.New("invalid build")
	ErrNotFound      = errors.New("not found")
	ErrDevice        = errors.New("device not found")
	ErrDeviceOffline = errors.New("device offline")
	ErrConflict      = errors.New("build already finished")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Service struct {
	Store   *store.Store
	Sender  Sender
	Secrets *secrets.Service
	Ops     *oplogs.Service
}

func New(st *store.Store, sender Sender, sec *secrets.Service) *Service {
	return &Service{Store: st, Sender: sender, Secrets: sec}
}

type CreateSourceRequest struct {
	UserID             string
	Type               string
	Name               string
	URL                string
	Branch             string
	GitTag             string
	Revision           string
	CredentialSecretID string
}

type CreateBuildRequest struct {
	UserID           string
	SourceID         string
	DeviceID         string
	Dockerfile       string
	Context          string
	Tag              string
	TimeoutSeconds   int
	RegistrySecretID string
}

func (s *Service) CreateSource(ctx context.Context, req CreateSourceRequest) (*store.AppSource, error) {
	if err := validateSourceURL(req.URL); err != nil {
		return nil, err
	}
	typ := strings.TrimSpace(req.Type)
	if typ == "" {
		typ = "git"
	}
	if typ != "git" {
		return nil, fmt.Errorf("%w: type must be git", ErrValidation)
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" && strings.TrimSpace(req.GitTag) == "" {
		branch = "main"
	}
	if err := validateRef("branch", branch); err != nil {
		return nil, err
	}
	if err := validateRef("tag", req.GitTag); err != nil {
		return nil, err
	}
	if err := validateRef("revision", req.Revision); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultNameFromURL(req.URL)
	}
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name", ErrValidation)
	}
	cred := secretRef(req.CredentialSecretID)
	if cred != "" {
		if s.Secrets == nil {
			return nil, fmt.Errorf("%w: secrets unavailable", ErrValidation)
		}
		rec, err := s.Secrets.Get(ctx, req.UserID, cred)
		if err != nil {
			return nil, fmt.Errorf("%w: git credential secret", ErrValidation)
		}
		cred = rec.ID
	}
	src := &store.AppSource{
		ID: store.NewID(), UserID: req.UserID, Type: typ, Name: name, URL: strings.TrimSpace(req.URL),
		Branch: branch, GitTag: strings.TrimSpace(req.GitTag), Revision: strings.TrimSpace(req.Revision),
		CredentialSecretID: cred,
	}
	if err := s.Store.CreateAppSource(ctx, src); err != nil {
		return nil, err
	}
	return s.Store.GetAppSource(ctx, req.UserID, src.ID)
}

func (s *Service) GetSource(ctx context.Context, userID, id string) (*store.AppSource, error) {
	src, err := s.Store.GetAppSource(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return src, nil
}

func (s *Service) ListSources(ctx context.Context, userID string) ([]store.AppSource, error) {
	return s.Store.ListAppSources(ctx, userID)
}

func (s *Service) Create(ctx context.Context, req CreateBuildRequest) (*store.Build, error) {
	src, err := s.GetSource(ctx, req.UserID, req.SourceID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return nil, fmt.Errorf("%w: device_id required", ErrValidation)
	}
	dev, err := s.Store.GetDevice(ctx, req.UserID, req.DeviceID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrDevice
		}
		return nil, err
	}
	if s.Sender == nil || !s.Sender.IsOnline(dev.ID) {
		return nil, ErrDeviceOffline
	}
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	contextDir := req.Context
	if contextDir == "" {
		contextDir = "."
	}
	dockerfile, err = validateRelPath(dockerfile, "dockerfile")
	if err != nil {
		return nil, err
	}
	contextDir, err = validateRelPath(contextDir, "context")
	if err != nil {
		return nil, err
	}
	if err := validateImageTag(req.Tag); err != nil {
		return nil, err
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = protocol.DefaultBuildTimeout
	}
	if timeout > protocol.MaxBuildTimeout {
		timeout = protocol.MaxBuildTimeout
	}
	reg := secretRef(req.RegistrySecretID)
	if reg != "" {
		if s.Secrets == nil {
			return nil, fmt.Errorf("%w: secrets unavailable", ErrValidation)
		}
		rec, err := s.Secrets.Get(ctx, req.UserID, reg)
		if err != nil {
			return nil, fmt.Errorf("%w: registry secret", ErrValidation)
		}
		reg = rec.ID
	}
	b := &store.Build{
		ID: store.NewID(), UserID: req.UserID, SourceID: src.ID, DeviceID: dev.ID,
		Dockerfile: dockerfile, Context: contextDir, Tag: strings.TrimSpace(req.Tag),
		Status: protocol.BuildStatusQueued, RegistrySecretID: reg, TimeoutSeconds: timeout,
		TraceID: oplogs.ResolveTrace(ctx, ""),
	}
	if err := s.Store.CreateBuild(ctx, b); err != nil {
		return nil, err
	}
	s.log(ctx, b.ID, "stdout", fmt.Sprintf("queued source=%s tag=%s device=%s", src.ID, b.Tag, dev.ID), nil)
	if err := s.dispatch(ctx, b, src); err != nil {
		return s.Store.GetBuild(ctx, req.UserID, b.ID)
	}
	return s.Store.GetBuild(ctx, req.UserID, b.ID)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Build, error) {
	b, err := s.Store.GetBuild(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *Service) List(ctx context.Context, userID, sourceID, deviceID string) ([]store.Build, error) {
	return s.Store.ListBuilds(ctx, userID, sourceID, deviceID)
}

func (s *Service) Logs(ctx context.Context, userID, id string, limit int) ([]store.BuildLog, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	return s.Store.ListBuildLogs(ctx, id, limit)
}

func (s *Service) HandleAgentMessage(_ context.Context, _ string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeBuildResult:
		var res protocol.BuildResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		s.applyResult(res)
	case protocol.TypeBuildLogLine:
		var msg protocol.BuildLogLine
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		s.log(context.Background(), msg.BuildID, msg.Stream, msg.Message, nil)
	case protocol.TypeBuildProgress:
		var msg protocol.BuildProgress
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		s.applyProgress(msg)
	}
	return nil
}

func (s *Service) applyProgress(msg protocol.BuildProgress) {
	if msg.BuildID == "" || protocol.BuildTerminal(msg.Status) {
		return
	}
	switch msg.Status {
	case protocol.BuildStatusCloning, protocol.BuildStatusBuilding, protocol.BuildStatusPushing:
	default:
		return
	}
	ctx := context.Background()
	b, err := s.Store.GetBuildByID(ctx, msg.BuildID)
	if err != nil || protocol.BuildTerminal(b.Status) {
		return
	}
	b.Status = msg.Status
	if b.StartedAt == nil {
		now := time.Now().UTC()
		b.StartedAt = &now
	}
	_ = s.Store.UpdateBuild(ctx, b)
}

func (s *Service) applyResult(res protocol.BuildResult) {
	ctx := context.Background()
	b, err := s.Store.GetBuildByID(ctx, res.BuildID)
	if err != nil || protocol.BuildTerminal(b.Status) {
		return
	}
	now := time.Now().UTC()
	b.Status = res.Status
	if b.Status == "" {
		if res.OK {
			b.Status = protocol.BuildStatusCompleted
		} else {
			b.Status = protocol.BuildStatusFailed
		}
	}
	if !protocol.BuildTerminal(b.Status) {
		if res.OK {
			b.Status = protocol.BuildStatusCompleted
		} else {
			b.Status = protocol.BuildStatusFailed
		}
	}
	b.Error = deploy.SanitizeLogLine(res.Error)
	if res.Image != "" {
		b.Image = res.Image
	}
	if res.Revision != "" {
		b.GitRevision = res.Revision
	}
	b.FinishedAt = &now
	_ = s.Store.UpdateBuild(ctx, b)
	if b.Status == protocol.BuildStatusCompleted && b.GitRevision != "" {
		_ = s.Store.UpdateAppSourceRevision(ctx, b.UserID, b.SourceID, b.GitRevision)
	}
	for _, line := range res.LogLines {
		s.log(ctx, b.ID, "stdout", line, nil)
	}
}

func (s *Service) OnDeviceDisconnect(deviceID string) {
	ctx := context.Background()
	list, err := s.Store.ListInflightBuildsByDevice(ctx, deviceID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for i := range list {
		b := &list[i]
		if protocol.BuildTerminal(b.Status) {
			continue
		}
		b.Status = protocol.BuildStatusFailed
		b.Error = "agent disconnected"
		b.FinishedAt = &now
		_ = s.Store.UpdateBuild(ctx, b)
		s.log(ctx, b.ID, "stderr", "agent disconnected", nil)
	}
}

func (s *Service) dispatch(ctx context.Context, b *store.Build, src *store.AppSource) error {
	now := time.Now().UTC()
	b.StartedAt = &now
	b.Error = ""
	gitToken, gitErr := s.reveal(ctx, b.UserID, src.CredentialSecretID)
	if gitErr != nil {
		return s.fail(ctx, b, protocol.BuildStatusFailedClone, "git credential unavailable")
	}
	regUser, regToken, regErr := s.revealRegistry(ctx, b.UserID, b.RegistrySecretID)
	if regErr != nil {
		return s.fail(ctx, b, protocol.BuildStatusFailedPush, "registry credential unavailable")
	}
	secrets := compactSecrets(gitToken, regToken)
	spec := protocol.BuildSpec{
		BuildID: b.ID, URL: src.URL, Branch: src.Branch, GitTag: src.GitTag, Revision: src.Revision,
		Dockerfile: b.Dockerfile, Context: b.Context, Tag: b.Tag, TimeoutSeconds: b.TimeoutSeconds,
		GitToken: gitToken, RegistryUser: regUser, RegistryToken: regToken,
	}
	if err := s.Sender.SendJSON(b.DeviceID, protocol.BuildRun{
		Type: protocol.TypeBuildRun, RequestID: store.NewID(), BuildID: b.ID, Spec: spec,
	}); err != nil {
		return s.fail(ctx, b, protocol.BuildStatusFailed, deploy.RedactSecrets(err.Error(), secrets))
	}
	b.Status = protocol.BuildStatusCloning
	_ = s.Store.UpdateBuild(ctx, b)
	s.log(ctx, b.ID, "stdout", fmt.Sprintf("dispatched device=%s url=%s", b.DeviceID, src.URL), secrets)
	return nil
}

func (s *Service) fail(ctx context.Context, b *store.Build, status, errMsg string) error {
	now := time.Now().UTC()
	b.Status = status
	b.Error = errMsg
	b.FinishedAt = &now
	_ = s.Store.UpdateBuild(ctx, b)
	s.log(ctx, b.ID, "stderr", errMsg, nil)
	return errors.New(errMsg)
}

func (s *Service) reveal(ctx context.Context, userID, secretID string) (string, error) {
	if secretID == "" {
		return "", nil
	}
	if s.Secrets == nil {
		return "", errors.New("secrets unavailable")
	}
	return s.Secrets.Reveal(ctx, userID, secretID, 0)
}

func (s *Service) revealRegistry(ctx context.Context, userID, secretID string) (user, token string, err error) {
	raw, err := s.reveal(ctx, userID, secretID)
	if err != nil || raw == "" {
		return "", raw, err
	}
	if u, p, ok := strings.Cut(raw, ":"); ok && u != "" && p != "" {
		return u, p, nil
	}
	return "", raw, nil
}

func (s *Service) log(ctx context.Context, buildID, stream, message string, secrets []string) {
	msg := deploy.RedactSecrets(message, secrets)
	_ = s.Store.AppendBuildLog(ctx, buildID, stream, msg)
	b, err := s.Store.GetBuildByID(ctx, buildID)
	if err != nil {
		return
	}
	s.Ops.Emit(ctx, oplogs.Event{
		UserID: b.UserID, Source: oplogs.SourceBuild, Level: oplogs.LevelFromStream(stream),
		Message: msg, TraceID: b.TraceID, DeviceID: b.DeviceID, BuildID: b.ID,
	})
}

func compactSecrets(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
