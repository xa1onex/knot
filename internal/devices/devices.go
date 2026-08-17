package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrInvalidRegToken = errors.New("invalid registration token")
	ErrRegTokenUsed    = errors.New("registration token already used")
	ErrRegTokenExpired = errors.New("registration token expired")
)

type Service struct {
	Store *store.Store
	Auth  *auth.Service
}

func (s *Service) List(ctx context.Context, userID string) ([]store.Device, error) {
	return s.Store.ListDevices(ctx, userID)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Device, error) {
	return s.Store.GetDevice(ctx, userID, id)
}

func (s *Service) Register(ctx context.Context, req protocol.RegisterRequest) (*protocol.RegisterResponse, string, error) {
	tok, err := s.Store.GetRegistrationTokenByHash(ctx, auth.HashToken(req.RegistrationToken))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, "", ErrInvalidRegToken
		}
		return nil, "", err
	}
	if tok.UsedAt != nil {
		return nil, "", ErrRegTokenUsed
	}
	if time.Now().UTC().After(tok.ExpiresAt) {
		return nil, "", ErrRegTokenExpired
	}
	if req.PublicKey == "" {
		return nil, "", fmt.Errorf("public_key required")
	}

	name := req.Name
	if name == "" {
		name = tok.NameHint
	}
	if name == "" {
		name = req.Hostname
	}
	if name == "" {
		name = "device"
	}

	deviceToken, err := auth.RandomToken("knot_dev_", 32)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	d := &store.Device{
		ID:              store.NewID(),
		UserID:          tok.UserID,
		Name:            name,
		PublicKey:       req.PublicKey,
		DeviceTokenHash: auth.HashToken(deviceToken),
		Hostname:        req.Hostname,
		OS:              req.OS,
		Arch:            req.Arch,
		Online:          false,
		CreatedAt:       now,
	}
	if err := s.Store.CreateDevice(ctx, d); err != nil {
		return nil, "", err
	}
	if err := s.Store.MarkRegistrationTokenUsed(ctx, tok.ID); err != nil {
		return nil, "", err
	}
	return &protocol.RegisterResponse{
		DeviceID:    d.ID,
		DeviceToken: deviceToken,
		Name:        d.Name,
	}, tok.UserID, nil
}

func (s *Service) Touch(ctx context.Context, deviceID string, online bool, t protocol.Telemetry) error {
	if err := s.Store.UpdateDevicePresence(ctx, deviceID, online, t.Hostname, t.OS, t.Arch, t.CPUs, t.RAMMB, t.Version); err != nil {
		return err
	}
	if t.Compute == nil {
		return nil
	}
	raw, err := json.Marshal(t.Compute)
	if err != nil {
		return err
	}
	return s.Store.UpsertDeviceCompute(ctx, deviceID, string(raw), time.Now().UTC())
}

func (s *Service) MarkStaleOffline(ctx context.Context, timeout time.Duration) error {
	_, err := s.Store.MarkStaleDevicesOffline(ctx, time.Now().UTC().Add(-timeout))
	return err
}

func (s *Service) Revoke(ctx context.Context, userID, deviceID string) error {
	return s.Store.RevokeDevice(ctx, userID, deviceID)
}
