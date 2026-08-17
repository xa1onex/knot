package secrets

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
)

var (
	ErrValidation = errors.New("invalid secret")
	ErrNotFound   = errors.New("secret not found")
	ErrConflict   = errors.New("secret already exists")
)

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_./-]{0,127}$`)

type Record struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Service struct {
	Store *store.Store
	Key   []byte
}

func New(st *store.Store, key []byte) *Service {
	return &Service{Store: st, Key: key}
}

func (s *Service) Create(ctx context.Context, userID, name, value string) (*Record, error) {
	name = strings.TrimSpace(name)
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid name", ErrValidation)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: value required", ErrValidation)
	}
	if len(value) > 64*1024 {
		return nil, fmt.Errorf("%w: value too large", ErrValidation)
	}
	if _, err := s.Store.GetSecretByName(ctx, userID, name); err == nil {
		return nil, ErrConflict
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	ct, err := Seal(s.Key, value)
	if err != nil {
		return nil, err
	}
	sec := &store.Secret{UserID: userID, Name: name, Version: 1}
	if err := s.Store.CreateSecret(ctx, sec, ct); err != nil {
		return nil, err
	}
	return toRecord(sec), nil
}

func (s *Service) Rotate(ctx context.Context, userID, idOrName, value string) (*Record, error) {
	if value == "" {
		return nil, fmt.Errorf("%w: value required", ErrValidation)
	}
	if len(value) > 64*1024 {
		return nil, fmt.Errorf("%w: value too large", ErrValidation)
	}
	sec, err := s.lookup(ctx, userID, idOrName)
	if err != nil {
		return nil, err
	}
	ct, err := Seal(s.Key, value)
	if err != nil {
		return nil, err
	}
	out, err := s.Store.RotateSecret(ctx, userID, sec.ID, ct)
	if err != nil {
		return nil, err
	}
	return toRecord(out), nil
}

func (s *Service) Get(ctx context.Context, userID, idOrName string) (*Record, error) {
	sec, err := s.lookup(ctx, userID, idOrName)
	if err != nil {
		return nil, err
	}
	return toRecord(sec), nil
}

func (s *Service) List(ctx context.Context, userID string) ([]Record, error) {
	list, err := s.Store.ListSecrets(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(list))
	for i := range list {
		out = append(out, *toRecord(&list[i]))
	}
	return out, nil
}

// Reveal decrypts a pinned version. Callers must not return this over the API.
func (s *Service) Reveal(ctx context.Context, userID, secretID string, version int) (string, error) {
	sec, err := s.Store.GetSecret(ctx, userID, secretID)
	if err != nil {
		if store.IsNotFound(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if version <= 0 {
		version = sec.Version
	}
	ver, err := s.Store.GetSecretVersion(ctx, sec.ID, version)
	if err != nil {
		if store.IsNotFound(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return Open(s.Key, ver.Ciphertext)
}

func (s *Service) lookup(ctx context.Context, userID, idOrName string) (*store.Secret, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return nil, ErrNotFound
	}
	sec, err := s.Store.GetSecret(ctx, userID, idOrName)
	if err == nil {
		return sec, nil
	}
	if !store.IsNotFound(err) {
		return nil, err
	}
	sec, err = s.Store.GetSecretByName(ctx, userID, idOrName)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sec, nil
}

func toRecord(sec *store.Secret) *Record {
	return &Record{
		ID: sec.ID, Name: sec.Name, Version: sec.Version,
		CreatedAt: sec.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: sec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
