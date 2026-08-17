package client

import (
	"context"
	"net/http"
	"net/url"
)

type HostedService struct {
	ID               string   `json:"id"`
	DeviceID         string   `json:"device_id"`
	DeviceName       string   `json:"device_name"`
	DeviceOnline     bool     `json:"device_online"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Protocol         string   `json:"protocol"`
	Port             int      `json:"port"`
	Bind             string   `json:"bind"`
	Listen           string   `json:"listen"`
	Status           string   `json:"status"`
	Registered       bool     `json:"registered"`
	AgentOnline      bool     `json:"agent_online"`
	TunnelConnected  bool     `json:"tunnel_connected"`
	BackendReachable bool     `json:"backend_reachable"`
	EdgeDeviceID     string   `json:"edge_device_id,omitempty"`
	EdgeDeviceName   string   `json:"edge_device_name,omitempty"`
	EdgeOnline       bool     `json:"edge_online"`
	Hostnames        []string `json:"hostnames,omitempty"`
	HealthError      string   `json:"health_error,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

type ServiceNode struct {
	DeviceID   string          `json:"device_id"`
	DeviceName string          `json:"device_name"`
	Online     bool            `json:"online"`
	Services   []HostedService `json:"services"`
}

type RegisterServiceRequest struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port     int    `json:"port"`
	Bind     string `json:"bind,omitempty"`
}

type UpdateServiceRequest struct {
	Name     *string `json:"name,omitempty"`
	Kind     *string `json:"kind,omitempty"`
	Protocol *string `json:"protocol,omitempty"`
	Port     *int    `json:"port,omitempty"`
	Bind     *string `json:"bind,omitempty"`
}

func (c *Client) ListServices(ctx context.Context, deviceID string) ([]HostedService, error) {
	path := "/v1/services"
	if deviceID != "" {
		path += "?" + url.Values{"device_id": {deviceID}}.Encode()
	}
	var out struct {
		Services []HostedService `json:"services"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Services == nil {
		return []HostedService{}, nil
	}
	return out.Services, nil
}

func (c *Client) ServicesTree(ctx context.Context) ([]ServiceNode, error) {
	var out struct {
		Nodes []ServiceNode `json:"nodes"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/services/tree", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Nodes == nil {
		return []ServiceNode{}, nil
	}
	return out.Nodes, nil
}

func (c *Client) RegisterService(ctx context.Context, req RegisterServiceRequest) (*HostedService, error) {
	var out HostedService
	if err := c.do(ctx, http.MethodPost, "/v1/services", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetService(ctx context.Context, id string) (*HostedService, error) {
	var out HostedService
	if err := c.do(ctx, http.MethodGet, "/v1/services/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateService(ctx context.Context, id string, req UpdateServiceRequest) (*HostedService, error) {
	var out HostedService
	if err := c.do(ctx, http.MethodPatch, "/v1/services/"+id, req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteService(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/services/"+id, nil, &map[string]any{}, true)
}

func (c *Client) ServiceHealth(ctx context.Context, id string) (*HostedService, error) {
	var out HostedService
	if err := c.do(ctx, http.MethodGet, "/v1/services/"+id+"/health", nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

type EdgeRoute struct {
	ID             string `json:"id"`
	Hostname       string `json:"hostname"`
	ServiceID      string `json:"service_id"`
	ServiceName    string `json:"service_name"`
	DeviceID       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	EdgeDeviceID   string `json:"edge_device_id,omitempty"`
	EdgeDeviceName string `json:"edge_device_name,omitempty"`
	TLSMode        string `json:"tls_mode"`
	Listen         string `json:"listen"`
	CreatedAt      string `json:"created_at"`
}

type CreateRouteRequest struct {
	Hostname     string `json:"hostname"`
	ServiceID    string `json:"service_id"`
	EdgeDeviceID string `json:"edge_device_id,omitempty"`
	TLSMode      string `json:"tls_mode,omitempty"`
}

func (c *Client) ListRoutes(ctx context.Context) ([]EdgeRoute, error) {
	var out struct {
		Routes []EdgeRoute `json:"routes"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/routes", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Routes == nil {
		return []EdgeRoute{}, nil
	}
	return out.Routes, nil
}

func (c *Client) CreateRoute(ctx context.Context, req CreateRouteRequest) (*EdgeRoute, error) {
	var out EdgeRoute
	if err := c.do(ctx, http.MethodPost, "/v1/routes", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/routes/"+id, nil, &map[string]any{}, true)
}
