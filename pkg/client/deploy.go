package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type Deployment struct {
	ID               string               `json:"id"`
	DeviceID         string               `json:"device_id"`
	DeviceName       string               `json:"device_name"`
	DeviceOnline     bool                 `json:"device_online"`
	ServiceID        string               `json:"service_id"`
	Name             string               `json:"name"`
	Runtime          string               `json:"runtime"`
	Image            string               `json:"image"`
	Port             int                  `json:"port"`
	Bind             string               `json:"bind"`
	Listen           string               `json:"listen"`
	HealthPath       string               `json:"health_path"`
	Revision         int                  `json:"revision"`
	Status           string               `json:"status"`
	ContainerID      string               `json:"container_id"`
	PrevDeploymentID string               `json:"prev_deployment_id"`
	Active           bool                 `json:"active"`
	HealthOK         bool                 `json:"health_ok"`
	Error            string               `json:"error"`
	Env              map[string]string    `json:"env"`
	EnvironmentID    string               `json:"environment_id"`
	Secrets          map[string]SecretPin `json:"secrets,omitempty"`
	ReleaseID        string               `json:"release_id,omitempty"`
	TraceID          string               `json:"trace_id,omitempty"`
	CreatedAt        string               `json:"created_at"`
	UpdatedAt        string               `json:"updated_at"`
}

type SecretPin struct {
	SecretID string `json:"secret_id"`
	Name     string `json:"name"`
	Version  int    `json:"version"`
}

type CreateDeploymentRequest struct {
	DeviceID      string            `json:"device_id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Runtime       string            `json:"runtime,omitempty"`
	Port          int               `json:"port"`
	Bind          string            `json:"bind,omitempty"`
	HealthPath    string            `json:"health_path,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	EdgeDeviceID  string            `json:"edge_device_id,omitempty"`
	Environment   string            `json:"environment,omitempty"`
	EnvironmentID string            `json:"environment_id,omitempty"`
	Project       string            `json:"project,omitempty"`
}

type DeploymentLog struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) ListDeployments(ctx context.Context, deviceID, name string) ([]Deployment, error) {
	q := url.Values{}
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	if name != "" {
		q.Set("name", name)
	}
	path := "/v1/deployments"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Deployments []Deployment `json:"deployments"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Deployments == nil {
		return []Deployment{}, nil
	}
	return out.Deployments, nil
}

func (c *Client) CreateDeployment(ctx context.Context, req CreateDeploymentRequest) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodPost, "/v1/deployments", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetDeployment(ctx context.Context, id string) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodGet, "/v1/deployments/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StopDeployment(ctx context.Context, id string) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodPost, "/v1/deployments/"+id+"/stop", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RestartDeployment(ctx context.Context, id string) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodPost, "/v1/deployments/"+id+"/restart", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RollbackDeployment(ctx context.Context, id string) (*Deployment, error) {
	var out Deployment
	if err := c.do(ctx, http.MethodPost, "/v1/deployments/"+id+"/rollback", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeploymentLogs(ctx context.Context, id string, limit int) ([]DeploymentLog, error) {
	path := "/v1/deployments/" + id + "/logs"
	if limit > 0 {
		path += "?" + url.Values{"limit": {strconv.Itoa(limit)}}.Encode()
	}
	var out struct {
		Logs []DeploymentLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Logs == nil {
		return []DeploymentLog{}, nil
	}
	return out.Logs, nil
}
