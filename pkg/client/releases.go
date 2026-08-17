package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type Release struct {
	ID                   string               `json:"id"`
	Number               int                  `json:"number"`
	Service              string               `json:"service"`
	Image                string               `json:"image"`
	EnvironmentID        string               `json:"environment_id"`
	Environment          string               `json:"environment"`
	ConfigVersion        string               `json:"config_version"`
	Secrets              map[string]SecretPin `json:"secrets"`
	Status               string               `json:"status"`
	CreatedBy            string               `json:"created_by"`
	DeviceID             string               `json:"device_id"`
	DeviceName           string               `json:"device_name"`
	Port                 int                  `json:"port"`
	Bind                 string               `json:"bind"`
	HealthPath           string               `json:"health_path"`
	HealthTimeoutSeconds int                  `json:"health_timeout_seconds"`
	HealthRetries        int                  `json:"health_retries"`
	HealthExpectedStatus int                  `json:"health_expected_status"`
	BuildID              string               `json:"build_id"`
	JobID                string               `json:"job_id"`
	DeploymentID         string               `json:"deployment_id"`
	PrevReleaseID        string               `json:"prev_release_id"`
	Current              bool                 `json:"current"`
	Error                string               `json:"error"`
	TraceID              string               `json:"trace_id,omitempty"`
	CreatedAt            string               `json:"created_at"`
	UpdatedAt            string               `json:"updated_at"`
}

type CreateReleaseRequest struct {
	Service              string `json:"service"`
	Image                string `json:"image,omitempty"`
	Environment          string `json:"environment,omitempty"`
	EnvironmentID        string `json:"environment_id,omitempty"`
	Project              string `json:"project,omitempty"`
	DeviceID             string `json:"device_id,omitempty"`
	Port                 int    `json:"port,omitempty"`
	Bind                 string `json:"bind,omitempty"`
	HealthPath           string `json:"health_path,omitempty"`
	HealthTimeoutSeconds int    `json:"health_timeout_seconds,omitempty"`
	HealthRetries        int    `json:"health_retries,omitempty"`
	HealthExpectedStatus int    `json:"health_expected_status,omitempty"`
	Hostname             string `json:"hostname,omitempty"`
	EdgeDeviceID         string `json:"edge_device_id,omitempty"`
	BuildID              string `json:"build_id,omitempty"`
	JobID                string `json:"job_id,omitempty"`
}

type ReleaseLog struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) ListReleases(ctx context.Context, service string) ([]Release, error) {
	path := "/v1/releases"
	if service != "" {
		path += "?" + url.Values{"service": {service}}.Encode()
	}
	var out struct {
		Releases []Release `json:"releases"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Releases == nil {
		return []Release{}, nil
	}
	return out.Releases, nil
}

func (c *Client) CreateRelease(ctx context.Context, req CreateReleaseRequest) (*Release, error) {
	var out Release
	if err := c.do(ctx, http.MethodPost, "/v1/releases", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetRelease(ctx context.Context, id string) (*Release, error) {
	var out Release
	if err := c.do(ctx, http.MethodGet, "/v1/releases/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeployRelease(ctx context.Context, id, deviceID string, port int) (*Release, error) {
	var out Release
	body := map[string]any{}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	if port > 0 {
		body["port"] = port
	}
	if err := c.do(ctx, http.MethodPost, "/v1/releases/"+id+"/deploy", body, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RollbackRelease(ctx context.Context, id string) (*Release, error) {
	var out Release
	if err := c.do(ctx, http.MethodPost, "/v1/releases/"+id+"/rollback", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ReleaseLogs(ctx context.Context, id string, limit int) ([]ReleaseLog, error) {
	path := "/v1/releases/" + id + "/logs"
	if limit > 0 {
		path += "?" + url.Values{"limit": {strconv.Itoa(limit)}}.Encode()
	}
	var out struct {
		Logs []ReleaseLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Logs == nil {
		return []ReleaseLog{}, nil
	}
	return out.Logs, nil
}
