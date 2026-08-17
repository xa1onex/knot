// Package services is Stage 7.1 — a registry of workloads bound to Node devices.
// Control Plane stores metadata only; processes stay on the node (deploy is 7.4).
package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/knot-infra/knot/internal/store"
)

var (
	ErrValidation = errors.New("invalid service")
	ErrConflict   = errors.New("service already exists")
	ErrNotFound   = errors.New("service not found")
	ErrDevice     = errors.New("device not found")
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

var kinds = map[string]bool{
	"web": true, "api": true, "database": true, "worker": true, "other": true,
}

var protocols = map[string]bool{
	"http": true, "https": true, "tcp": true, "udp": true,
}

type Service struct {
	Store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{Store: st}
}

type RegisterRequest struct {
	UserID   string
	DeviceID string
	Name     string
	Kind     string
	Protocol string
	Port     int
	Bind     string
}

type UpdateRequest struct {
	Name     *string
	Kind     *string
	Protocol *string
	Port     *int
	Bind     *string
}

type NodeServices struct {
	DeviceID   string          `json:"device_id"`
	DeviceName string          `json:"device_name"`
	Online     bool            `json:"online"`
	Services   []store.Service `json:"services"`
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*store.Service, error) {
	if err := s.requireDevice(ctx, req.UserID, req.DeviceID); err != nil {
		return nil, err
	}
	name, err := normalizeName(req.Name)
	if err != nil {
		return nil, err
	}
	kind, err := normalizeKind(req.Kind)
	if err != nil {
		return nil, err
	}
	proto, err := normalizeProtocol(req.Protocol, kind)
	if err != nil {
		return nil, err
	}
	port, err := normalizePort(req.Port)
	if err != nil {
		return nil, err
	}
	bind, err := normalizeBind(req.Bind)
	if err != nil {
		return nil, err
	}
	if existing, err := s.Store.GetServiceByName(ctx, req.UserID, req.DeviceID, name); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: %s", ErrConflict, name)
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	svc := &store.Service{
		UserID:   req.UserID,
		DeviceID: req.DeviceID,
		Name:     name,
		Kind:     kind,
		Protocol: proto,
		Port:     port,
		Bind:     bind,
		Status:   store.ServiceStatusRegistered,
	}
	if err := s.Store.CreateService(ctx, svc); err != nil {
		return nil, err
	}
	return s.Store.GetService(ctx, req.UserID, svc.ID)
}

func (s *Service) List(ctx context.Context, userID, deviceID string) ([]store.Service, error) {
	list, err := s.Store.ListServices(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []store.Service{}
	}
	return list, nil
}

func (s *Service) Tree(ctx context.Context, userID string) ([]NodeServices, error) {
	devs, err := s.Store.ListDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	list, err := s.Store.ListServices(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	byDev := map[string][]store.Service{}
	for i := range list {
		byDev[list[i].DeviceID] = append(byDev[list[i].DeviceID], list[i])
	}
	out := make([]NodeServices, 0, len(devs))
	for i := range devs {
		d := &devs[i]
		if d.RevokedAt != nil {
			continue
		}
		svcs := byDev[d.ID]
		if svcs == nil {
			svcs = []store.Service{}
		}
		out = append(out, NodeServices{
			DeviceID:   d.ID,
			DeviceName: d.Name,
			Online:     d.Online,
			Services:   svcs,
		})
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Service, error) {
	svc, err := s.Store.GetService(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return svc, nil
}

func (s *Service) Update(ctx context.Context, userID, id string, req UpdateRequest) (*store.Service, error) {
	svc, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		name, err := normalizeName(*req.Name)
		if err != nil {
			return nil, err
		}
		if name != svc.Name {
			if existing, err := s.Store.GetServiceByName(ctx, userID, svc.DeviceID, name); err == nil && existing.ID != svc.ID {
				return nil, fmt.Errorf("%w: %s", ErrConflict, name)
			} else if err != nil && !store.IsNotFound(err) {
				return nil, err
			}
		}
		svc.Name = name
	}
	if req.Kind != nil {
		kind, err := normalizeKind(*req.Kind)
		if err != nil {
			return nil, err
		}
		svc.Kind = kind
	}
	if req.Protocol != nil {
		proto, err := normalizeProtocol(*req.Protocol, svc.Kind)
		if err != nil {
			return nil, err
		}
		svc.Protocol = proto
	}
	if req.Port != nil {
		port, err := normalizePort(*req.Port)
		if err != nil {
			return nil, err
		}
		svc.Port = port
	}
	if req.Bind != nil {
		bind, err := normalizeBind(*req.Bind)
		if err != nil {
			return nil, err
		}
		svc.Bind = bind
	}
	if err := s.Store.UpdateService(ctx, svc); err != nil {
		return nil, err
	}
	return s.Store.GetService(ctx, userID, id)
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	if err := s.Store.DeleteService(ctx, userID, id); err != nil {
		if store.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Service) requireDevice(ctx context.Context, userID, deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("%w: device_id required", ErrValidation)
	}
	d, err := s.Store.GetDevice(ctx, userID, deviceID)
	if err != nil || d.RevokedAt != nil {
		return ErrDevice
	}
	return nil
}

func normalizeName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("%w: name must be a lowercase slug (e.g. web-app)", ErrValidation)
	}
	return name, nil
}

// NormalizeDeployName validates a deployment/service slug (exported for deploy package).
func NormalizeDeployName(name string) (string, error) {
	return normalizeName(name)
}

func normalizeKind(kind string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "other"
	}
	if !kinds[kind] {
		return "", fmt.Errorf("%w: kind must be web, api, database, worker, or other", ErrValidation)
	}
	return kind, nil
}

func normalizeProtocol(proto, kind string) (string, error) {
	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto == "" {
		if kind == "database" {
			proto = "tcp"
		} else {
			proto = "http"
		}
	}
	if !protocols[proto] {
		return "", fmt.Errorf("%w: protocol must be http, https, tcp, or udp", ErrValidation)
	}
	return proto, nil
}

func normalizePort(port int) (int, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%w: port must be 1–65535", ErrValidation)
	}
	return port, nil
}

func normalizeBind(bind string) (string, error) {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		bind = "127.0.0.1"
	}
	if bind == "localhost" {
		return "127.0.0.1", nil
	}
	if ip := net.ParseIP(bind); ip != nil {
		return bind, nil
	}
	host, _, err := net.SplitHostPort(bind)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return host, nil
		}
	}
	if _, err := strconv.Atoi(bind); err == nil {
		return "", fmt.Errorf("%w: bind must be an address, not a port", ErrValidation)
	}
	return "", fmt.Errorf("%w: bind must be an IP address (default 127.0.0.1)", ErrValidation)
}
