package edge

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

type CreateRouteRequest struct {
	UserID       string
	Hostname     string
	ServiceID    string
	EdgeDeviceID string
	TLSMode      string
}

func normalizeTLSMode(mode string) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return protocol.TLSModeEdgeTerminate, nil
	}
	if mode != protocol.TLSModeEdgeTerminate && mode != protocol.TLSModeOriginTLS {
		return "", fmt.Errorf("%w: tls_mode must be edge_terminate or origin_tls", services.ErrValidation)
	}
	return mode, nil
}

func (p *Proxy) CreateRoute(ctx context.Context, req CreateRouteRequest) (*store.EdgeRoute, error) {
	host, err := normalizeHostname(req.Hostname)
	if err != nil {
		return nil, err
	}
	tlsMode, err := normalizeTLSMode(req.TLSMode)
	if err != nil {
		return nil, err
	}
	svc, err := p.Store.GetService(ctx, req.UserID, req.ServiceID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, services.ErrNotFound
		}
		return nil, err
	}
	switch tlsMode {
	case protocol.TLSModeEdgeTerminate:
		if svc.Protocol != "http" && svc.Protocol != "https" {
			return nil, fmt.Errorf("%w: edge_terminate routes require an HTTP service", services.ErrValidation)
		}
	case protocol.TLSModeOriginTLS:
		if svc.Protocol != "https" && svc.Protocol != "tcp" {
			return nil, fmt.Errorf("%w: origin_tls routes require https or tcp service", services.ErrValidation)
		}
	}
	if req.EdgeDeviceID != "" {
		d, err := p.Store.GetDevice(ctx, req.UserID, req.EdgeDeviceID)
		if err != nil || d.RevokedAt != nil {
			return nil, services.ErrDevice
		}
	}
	if existing, err := p.Store.GetEdgeRouteByHost(ctx, host); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: hostname %s", services.ErrConflict, host)
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	rt := &store.EdgeRoute{
		UserID:       req.UserID,
		Hostname:     host,
		ServiceID:    req.ServiceID,
		EdgeDeviceID: req.EdgeDeviceID,
		TLSMode:      tlsMode,
	}
	if err := p.Store.CreateEdgeRoute(ctx, rt); err != nil {
		return nil, err
	}
	return p.Store.GetEdgeRoute(ctx, req.UserID, rt.ID)
}

func (p *Proxy) ListRoutes(ctx context.Context, userID string) ([]store.EdgeRoute, error) {
	list, err := p.Store.ListEdgeRoutes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []store.EdgeRoute{}
	}
	return list, nil
}

func (p *Proxy) DeleteRoute(ctx context.Context, userID, id string) error {
	if err := p.Store.DeleteEdgeRoute(ctx, userID, id); err != nil {
		if store.IsNotFound(err) {
			return services.ErrNotFound
		}
		return err
	}
	return nil
}

func normalizeHostname(host string) (string, error) {
	host = NormalizeHost(host)
	if host == "localhost" {
		return host, nil
	}
	if !hostRe.MatchString(host) || len(host) > 253 {
		return "", fmt.Errorf("%w: hostname must be a DNS name (e.g. example.com)", services.ErrValidation)
	}
	return host, nil
}
