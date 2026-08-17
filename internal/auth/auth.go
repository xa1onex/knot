package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/permissions"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLocked             = errors.New("account locked")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrExpired            = errors.New("token expired")
	ErrRevoked            = errors.New("token revoked")
)

type IdentityKind string

const (
	KindUser   IdentityKind = "user"
	KindAPI    IdentityKind = "api"
	KindAI     IdentityKind = "ai_session"
	KindDevice IdentityKind = "device"
)

const (
	MaxLoginFails = 8
	LoginLockFor  = 15 * time.Minute
)

type Identity struct {
	Kind        IdentityKind
	UserID      string
	Email       string
	DeviceID    string
	CredID      string
	CredName    string
	Actor       string
	Scopes      []string
	ParentEmail string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
}

func (id Identity) Has(scope string) bool {
	return permissions.Check(id.Scopes, scope)
}

type Service struct {
	Store            *store.Store
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	DeviceSessionTTL time.Duration
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func RandomToken(prefix string, nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (s *Service) EnsureBootstrapAdmin(ctx context.Context, email, password string) error {
	n, err := s.Store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if email == "" || password == "" {
		return fmt.Errorf("bootstrap required: set KNOT_BOOTSTRAP_ADMIN and KNOT_BOOTSTRAP_PASSWORD")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.Store.CreateUser(ctx, email, hash)
	return err
}

func (s *Service) Login(ctx context.Context, email, password string) (*TokenPair, *store.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	if locked, until, err := s.Store.IsLoginLocked(ctx, key); err != nil {
		return nil, nil, err
	} else if locked {
		return nil, nil, fmt.Errorf("%w until %s", ErrLocked, until.UTC().Format(time.RFC3339))
	}
	user, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil {
		if store.IsNotFound(err) {
			_, _, _ = s.Store.RecordLoginFailure(ctx, key, MaxLoginFails, LoginLockFor)
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		_, _, _ = s.Store.RecordLoginFailure(ctx, key, MaxLoginFails, LoginLockFor)
		return nil, nil, ErrInvalidCredentials
	}
	_ = s.Store.ClearLoginAttempts(ctx, key)
	pair, err := s.issueUserTokens(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return pair, user, nil
}

func (s *Service) issueUserTokens(ctx context.Context, userID string) (*TokenPair, error) {
	access, err := RandomToken("knot_at_", 32)
	if err != nil {
		return nil, err
	}
	refresh, err := RandomToken("knot_rt_", 32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	accessTTL := s.AccessTokenTTL
	if accessTTL == 0 {
		accessTTL = time.Hour
	}
	refreshTTL := s.RefreshTokenTTL
	if refreshTTL == 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	if _, err := s.Store.CreateSession(ctx, userID, HashToken(access), now.Add(accessTTL)); err != nil {
		return nil, err
	}
	if err := s.Store.CreateRefreshToken(ctx, &store.RefreshToken{
		ID:        store.NewID(),
		UserID:    userID,
		TokenHash: HashToken(refresh),
		ExpiresAt: now.Add(refreshTTL),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(accessTTL.Seconds()),
	}, nil
}

func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	rt, err := s.Store.GetRefreshTokenByHash(ctx, HashToken(rawRefresh))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if rt.RevokedAt != nil {
		return nil, ErrRevoked
	}
	if time.Now().UTC().After(rt.ExpiresAt) {
		return nil, ErrExpired
	}
	_ = s.Store.RevokeRefreshToken(ctx, HashToken(rawRefresh))
	return s.issueUserTokens(ctx, rt.UserID)
}

func (s *Service) Logout(ctx context.Context, accessToken, refreshToken string) error {
	if accessToken != "" {
		_ = s.Store.DeleteSessionByTokenHash(ctx, HashToken(accessToken))
	}
	if refreshToken != "" {
		_ = s.Store.RevokeRefreshToken(ctx, HashToken(refreshToken))
	}
	return nil
}

func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	_ = s.Store.DeleteAllSessionsForUser(ctx, userID)
	return s.Store.RevokeAllRefreshTokens(ctx, userID)
}

func (s *Service) CreateAPICredential(ctx context.Context, userID, name string, scopes []string, expires *time.Time) (raw string, cred *store.Credential, err error) {
	return s.createCredential(ctx, userID, name, scopes, expires, store.CredentialKindAPI)
}

func (s *Service) CreateAICredential(ctx context.Context, userID, name string, scopes []string, expires *time.Time) (raw string, cred *store.Credential, err error) {
	return s.createCredential(ctx, userID, name, scopes, expires, store.CredentialKindAI)
}

func (s *Service) createCredential(ctx context.Context, userID, name string, scopes []string, expires *time.Time, kind string) (raw string, cred *store.Credential, err error) {
	prefix := "knot_api_"
	if kind == store.CredentialKindAI {
		prefix = "knot_ai_"
	}
	raw, err = RandomToken(prefix, 32)
	if err != nil {
		return "", nil, err
	}
	cred = &store.Credential{
		ID:          store.NewID(),
		UserID:      userID,
		Name:        name,
		TokenHash:   HashToken(raw),
		TokenPrefix: raw[:min(12, len(raw))],
		Kind:        kind,
		Scopes:      scopes,
		ExpiresAt:   expires,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Store.CreateCredential(ctx, cred); err != nil {
		return "", nil, err
	}
	return raw, cred, nil
}

func (s *Service) RotateAPICredential(ctx context.Context, userID, id string) (raw string, err error) {
	raw, err = RandomToken("knot_api_", 32)
	if err != nil {
		return "", err
	}
	prefix := raw[:min(12, len(raw))]
	if err := s.Store.RotateCredential(ctx, userID, id, HashToken(raw), prefix); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Service) CreateRegistrationToken(ctx context.Context, userID, nameHint string, ttl time.Duration) (raw string, tok *store.RegistrationToken, err error) {
	raw, err = RandomToken("knot_reg_", 24)
	if err != nil {
		return "", nil, err
	}
	tok = &store.RegistrationToken{
		ID:          store.NewID(),
		UserID:      userID,
		TokenHash:   HashToken(raw),
		TokenPrefix: raw[:min(12, len(raw))],
		NameHint:    nameHint,
		ExpiresAt:   time.Now().UTC().Add(ttl),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Store.CreateRegistrationToken(ctx, tok); err != nil {
		return "", nil, err
	}
	return raw, tok, nil
}

func (s *Service) IssueDeviceSession(ctx context.Context, device *store.Device) (raw string, err error) {
	raw, err = RandomToken("knot_ds_", 32)
	if err != nil {
		return "", err
	}
	ttl := s.DeviceSessionTTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()
	sess := &store.DeviceSession{
		ID:        store.NewID(),
		DeviceID:  device.ID,
		UserID:    device.UserID,
		TokenHash: HashToken(raw),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.Store.CreateDeviceSession(ctx, sess); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Service) ResolveBearer(ctx context.Context, raw string) (*Identity, error) {
	if raw == "" {
		return nil, ErrUnauthorized
	}
	hash := HashToken(raw)

	if sess, err := s.Store.GetSessionByTokenHash(ctx, hash); err == nil {
		if time.Now().UTC().After(sess.ExpiresAt) {
			return nil, ErrExpired
		}
		user, err := s.Store.GetUserByID(ctx, sess.UserID)
		if err != nil {
			return nil, ErrUnauthorized
		}
		return &Identity{
			Kind:   KindUser,
			UserID: user.ID,
			Email:  user.Email,
			Actor:  user.Email,
			Scopes: permissions.SessionScopes(),
		}, nil
	} else if !store.IsNotFound(err) {
		return nil, err
	}

	if cred, err := s.Store.GetCredentialByTokenHash(ctx, hash); err == nil {
		if cred.RevokedAt != nil {
			return nil, ErrRevoked
		}
		if cred.ExpiresAt != nil && time.Now().UTC().After(*cred.ExpiresAt) {
			return nil, ErrExpired
		}
		id := &Identity{
			Kind:      KindAPI,
			UserID:    cred.UserID,
			CredID:    cred.ID,
			CredName:  cred.Name,
			Actor:     fmt.Sprintf("credential:%s", cred.Name),
			Scopes:    cred.Scopes,
			ExpiresAt: cred.ExpiresAt,
			CreatedAt: cred.CreatedAt,
		}
		if cred.Kind == store.CredentialKindAI {
			id.Kind = KindAI
			parent := cred.UserID
			if user, uerr := s.Store.GetUserByID(ctx, cred.UserID); uerr == nil {
				id.Email = user.Email
				id.ParentEmail = user.Email
				parent = user.Email
			}
			id.Actor = fmt.Sprintf("ai-session:%s parent:%s", cred.Name, parent)
		}
		return id, nil
	} else if !store.IsNotFound(err) {
		return nil, err
	}

	if ds, err := s.Store.GetDeviceSessionByHash(ctx, hash); err == nil {
		if ds.RevokedAt != nil {
			return nil, ErrRevoked
		}
		if time.Now().UTC().After(ds.ExpiresAt) {
			return nil, ErrExpired
		}
		dev, err := s.Store.GetDeviceByID(ctx, ds.DeviceID)
		if err != nil {
			return nil, ErrUnauthorized
		}
		if dev.RevokedAt != nil {
			return nil, ErrRevoked
		}
		return &Identity{
			Kind:     KindDevice,
			UserID:   ds.UserID,
			DeviceID: ds.DeviceID,
			Actor:    fmt.Sprintf("device:%s", dev.Name),
			Scopes:   []string{},
		}, nil
	} else if !store.IsNotFound(err) {
		return nil, err
	}

	// Bootstrap device token (pre-challenge) — only for agent gateway handshake.
	if dev, err := s.Store.GetDeviceByTokenHash(ctx, hash); err == nil {
		if dev.RevokedAt != nil {
			return nil, ErrRevoked
		}
		return &Identity{
			Kind:     KindDevice,
			UserID:   dev.UserID,
			DeviceID: dev.ID,
			Actor:    fmt.Sprintf("device:%s", dev.Name),
			Scopes:   []string{},
		}, nil
	} else if !store.IsNotFound(err) {
		return nil, err
	}

	return nil, ErrUnauthorized
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
