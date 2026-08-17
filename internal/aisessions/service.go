package aisessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/permissions"
)

var (
	ErrValidation = errors.New("invalid AI session")
	ErrForbidden  = errors.New("forbidden")
	ErrNotFound   = errors.New("AI session not found")
)

const (
	DefaultTTL = 30 * time.Minute
	MaxTTL     = 7 * 24 * time.Hour
	MinTTL     = time.Second
)

type Service struct {
	Store *store.Store
	Auth  *auth.Service
}

func New(st *store.Store, authSvc *auth.Service) *Service {
	return &Service{Store: st, Auth: authSvc}
}

type CreateRequest struct {
	UserID       string
	Actor        string
	CreatorKind  auth.IdentityKind
	CreatorScope []string
	Name         string
	Scopes       []string
	TTLMinutes   int
	ExpiresIn    string
}

type Session struct {
	ID           string     `json:"id"`
	CredentialID string     `json:"credential_id"`
	Name         string     `json:"name"`
	CreatedBy    string     `json:"created_by"`
	Parent       string     `json:"parent"`
	Scopes       []string   `json:"scopes"`
	Status       string     `json:"status"`
	ExpiresAt    time.Time  `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	Token        string     `json:"token,omitempty"`
	Actor        string     `json:"actor,omitempty"`
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Session, error) {
	if req.CreatorKind == auth.KindAI {
		return nil, fmt.Errorf("%w: AI session cannot create another AI session", ErrForbidden)
	}
	if req.CreatorKind == auth.KindDevice {
		return nil, fmt.Errorf("%w: device tokens cannot create AI sessions", ErrForbidden)
	}
	scopes, msg := permissions.FilterGrantable(req.CreatorScope, req.Scopes)
	if msg != "" {
		return nil, fmt.Errorf("%w: %s", ErrValidation, msg)
	}
	ttl, err := ParseTTL(req.TTLMinutes, req.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "ai-session"
	}
	exp := time.Now().UTC().Add(ttl)
	raw, cred, err := s.Auth.CreateAICredential(ctx, req.UserID, name, scopes, &exp)
	if err != nil {
		return nil, err
	}
	parent := req.Actor
	if user, uerr := s.Store.GetUserByID(ctx, req.UserID); uerr == nil {
		parent = user.Email
	}
	out := view(cred, parent)
	out.Token = raw
	out.Actor = parent
	return out, nil
}

func (s *Service) List(ctx context.Context, userID string) ([]Session, error) {
	parent := userID
	if user, err := s.Store.GetUserByID(ctx, userID); err == nil {
		parent = user.Email
	}
	list, err := s.Store.ListCredentialsByKind(ctx, userID, store.CredentialKindAI)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(list))
	for i := range list {
		out = append(out, *view(&list[i], parent))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*Session, error) {
	cred, err := s.Store.GetCredentialByID(ctx, userID, strings.TrimSpace(id))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if cred.Kind != store.CredentialKindAI {
		return nil, ErrNotFound
	}
	parent := userID
	if user, uerr := s.Store.GetUserByID(ctx, userID); uerr == nil {
		parent = user.Email
	}
	return view(cred, parent), nil
}

func (s *Service) Revoke(ctx context.Context, userID, id string) error {
	sess, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.Store.RevokeCredential(ctx, userID, sess.CredentialID); err != nil {
		if store.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Service) Current(ctx context.Context, id *auth.Identity) (*Session, error) {
	if id == nil || id.Kind != auth.KindAI || id.CredID == "" {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id.UserID, id.CredID)
}

func view(cred *store.Credential, parent string) *Session {
	status := "active"
	if cred.RevokedAt != nil {
		status = "revoked"
	} else if cred.ExpiresAt != nil && time.Now().UTC().After(*cred.ExpiresAt) {
		status = "expired"
	}
	exp := time.Time{}
	if cred.ExpiresAt != nil {
		exp = *cred.ExpiresAt
	}
	return &Session{
		ID: cred.ID, CredentialID: cred.ID, Name: cred.Name,
		CreatedBy: parent, Parent: parent, Scopes: cred.Scopes, Status: status,
		ExpiresAt: exp, CreatedAt: cred.CreatedAt, RevokedAt: cred.RevokedAt, Actor: parent,
	}
}

func ParseTTL(minutes int, expiresIn string) (time.Duration, error) {
	var d time.Duration
	switch {
	case minutes > 0:
		d = time.Duration(minutes) * time.Minute
	case strings.TrimSpace(expiresIn) != "":
		raw := strings.TrimSpace(expiresIn)
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			var n int
			if _, perr := fmt.Sscanf(raw, "%d", &n); perr == nil && n > 0 {
				parsed = time.Duration(n) * time.Minute
			} else {
				return 0, fmt.Errorf("invalid ttl")
			}
		}
		d = parsed
	default:
		d = DefaultTTL
	}
	if d < MinTTL {
		return 0, fmt.Errorf("ttl too short")
	}
	if d > MaxTTL {
		return 0, fmt.Errorf("ttl too long")
	}
	return d, nil
}
