package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/store"
)

var (
	ErrValidation = errors.New("invalid environment")
	ErrNotFound   = errors.New("environment not found")
	ErrConflict   = errors.New("environment already exists")
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var projectRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type SecretRef struct {
	Key      string `json:"key"`
	SecretID string `json:"secret_id"`
	Name     string `json:"name"`
	Version  int    `json:"version,omitempty"`
}

type Record struct {
	ID        string            `json:"id"`
	Project   string            `json:"project"`
	Name      string            `json:"name"`
	Vars      map[string]string `json:"vars"`
	Secrets   []SecretRef       `json:"secrets"`
	Policy    map[string]string `json:"policy"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

type Service struct {
	Store   *store.Store
	Secrets *secrets.Service
}

func New(st *store.Store, sec *secrets.Service) *Service {
	return &Service{Store: st, Secrets: sec}
}

type CreateRequest struct {
	UserID  string
	Project string
	Name    string
	Vars    map[string]string
	Secrets map[string]string // env key → secret id or name
	Policy  map[string]string
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Record, error) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name", ErrValidation)
	}
	project := strings.ToLower(strings.TrimSpace(req.Project))
	if project != "" && !projectRe.MatchString(project) {
		return nil, fmt.Errorf("%w: invalid project", ErrValidation)
	}
	if err := validateVars(req.Vars); err != nil {
		return nil, err
	}
	refs, err := s.resolveRefs(ctx, req.UserID, req.Secrets)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.GetEnvironmentByName(ctx, req.UserID, project, name); err == nil {
		return nil, ErrConflict
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	e := &store.Environment{
		UserID: req.UserID, Project: project, Name: name,
		VarsJSON: marshalMap(req.Vars), SecretsJSON: marshalRefs(refs), PolicyJSON: marshalMap(req.Policy),
	}
	if err := s.Store.CreateEnvironment(ctx, e); err != nil {
		return nil, err
	}
	return s.toRecord(ctx, req.UserID, e)
}

func (s *Service) Update(ctx context.Context, userID, id string, vars map[string]string, secretMap map[string]string, policy map[string]string) (*Record, error) {
	e, err := s.Store.GetEnvironment(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if vars != nil {
		if err := validateVars(vars); err != nil {
			return nil, err
		}
		e.VarsJSON = marshalMap(vars)
	}
	if secretMap != nil {
		refs, err := s.resolveRefs(ctx, userID, secretMap)
		if err != nil {
			return nil, err
		}
		e.SecretsJSON = marshalRefs(refs)
	}
	if policy != nil {
		e.PolicyJSON = marshalMap(policy)
	}
	if err := s.Store.UpdateEnvironment(ctx, e); err != nil {
		return nil, err
	}
	return s.toRecord(ctx, userID, e)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*Record, error) {
	e, err := s.Store.GetEnvironment(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.toRecord(ctx, userID, e)
}

func (s *Service) Lookup(ctx context.Context, userID, project, name string) (*store.Environment, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	project = strings.ToLower(strings.TrimSpace(project))
	if name == "" {
		return nil, ErrNotFound
	}
	if project != "" {
		e, err := s.Store.GetEnvironmentByName(ctx, userID, project, name)
		if err == nil {
			return e, nil
		}
		if !store.IsNotFound(err) {
			return nil, err
		}
	}
	e, err := s.Store.GetEnvironmentByName(ctx, userID, "", name)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return e, nil
}

func (s *Service) List(ctx context.Context, userID, project string) ([]Record, error) {
	list, err := s.Store.ListEnvironments(ctx, userID, project)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(list))
	for i := range list {
		rec, err := s.toRecord(ctx, userID, &list[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, nil
}

func (s *Service) resolveRefs(ctx context.Context, userID string, m map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if len(m) > 32 {
		return nil, fmt.Errorf("%w: too many secret bindings", ErrValidation)
	}
	for k, v := range m {
		if !envKeyRe.MatchString(k) {
			return nil, fmt.Errorf("%w: invalid secret env key", ErrValidation)
		}
		v = strings.TrimPrefix(strings.TrimSpace(v), "secret://")
		if v == "" {
			return nil, fmt.Errorf("%w: secret reference required", ErrValidation)
		}
		if s.Secrets == nil {
			return nil, fmt.Errorf("%w: secrets unavailable", ErrValidation)
		}
		rec, err := s.Secrets.Get(ctx, userID, v)
		if err != nil {
			return nil, err
		}
		out[k] = rec.ID
	}
	return out, nil
}

func (s *Service) toRecord(ctx context.Context, userID string, e *store.Environment) (*Record, error) {
	refs := parseRefMap(e.SecretsJSON)
	secretsOut := make([]SecretRef, 0, len(refs))
	for k, id := range refs {
		item := SecretRef{Key: k, SecretID: id}
		if s.Secrets != nil {
			if rec, err := s.Secrets.Get(ctx, userID, id); err == nil {
				item.Name = rec.Name
				item.Version = rec.Version
			}
		}
		secretsOut = append(secretsOut, item)
	}
	return &Record{
		ID: e.ID, Project: e.Project, Name: e.Name,
		Vars: parseStringMap(e.VarsJSON), Secrets: secretsOut, Policy: parseStringMap(e.PolicyJSON),
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func validateVars(vars map[string]string) error {
	if len(vars) > 64 {
		return fmt.Errorf("%w: too many vars", ErrValidation)
	}
	for k, v := range vars {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("%w: invalid var key", ErrValidation)
		}
		if len(v) > 4096 {
			return fmt.Errorf("%w: var too large", ErrValidation)
		}
	}
	return nil
}

func marshalMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func marshalRefs(m map[string]string) string {
	return marshalMap(m)
}

func parseStringMap(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return map[string]string{}
	}
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]string{}
	}
	return out
}

func parseRefMap(raw string) map[string]string {
	return parseStringMap(raw)
}
