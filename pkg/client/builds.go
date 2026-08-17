package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type AppSource struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	Branch             string `json:"branch"`
	GitTag             string `json:"git_tag"`
	Revision           string `json:"revision"`
	CredentialSecretID string `json:"credential_secret_id,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type CreateSourceRequest struct {
	Type               string `json:"type,omitempty"`
	Name               string `json:"name,omitempty"`
	URL                string `json:"url"`
	Branch             string `json:"branch,omitempty"`
	GitTag             string `json:"git_tag,omitempty"`
	Revision           string `json:"revision,omitempty"`
	CredentialSecretID string `json:"credential_secret_id,omitempty"`
}

type Build struct {
	ID             string  `json:"id"`
	SourceID       string  `json:"source_id"`
	DeviceID       string  `json:"device_id"`
	DeviceName     string  `json:"device_name"`
	DeviceOnline   bool    `json:"device_online"`
	Dockerfile     string  `json:"dockerfile"`
	Context        string  `json:"context"`
	Tag            string  `json:"tag"`
	Image          string  `json:"image"`
	Status         string  `json:"status"`
	Error          string  `json:"error"`
	Revision       string  `json:"revision"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	TraceID        string  `json:"trace_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
	StartedAt      *string `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
}

type CreateBuildRequest struct {
	SourceID         string `json:"source_id"`
	DeviceID         string `json:"device_id"`
	Dockerfile       string `json:"dockerfile,omitempty"`
	Context          string `json:"context,omitempty"`
	Tag              string `json:"tag"`
	TimeoutSeconds   int    `json:"timeout_seconds,omitempty"`
	RegistrySecretID string `json:"registry_secret_id,omitempty"`
}

type BuildLog struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) ListSources(ctx context.Context) ([]AppSource, error) {
	var out struct {
		Sources []AppSource `json:"sources"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sources", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Sources == nil {
		return []AppSource{}, nil
	}
	return out.Sources, nil
}

func (c *Client) CreateSource(ctx context.Context, req CreateSourceRequest) (*AppSource, error) {
	var out AppSource
	if err := c.do(ctx, http.MethodPost, "/v1/sources", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSource(ctx context.Context, id string) (*AppSource, error) {
	var out AppSource
	if err := c.do(ctx, http.MethodGet, "/v1/sources/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListBuilds(ctx context.Context, sourceID, deviceID string) ([]Build, error) {
	path := "/v1/builds"
	q := url.Values{}
	if sourceID != "" {
		q.Set("source_id", sourceID)
	}
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Builds []Build `json:"builds"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Builds == nil {
		return []Build{}, nil
	}
	return out.Builds, nil
}

func (c *Client) CreateBuild(ctx context.Context, req CreateBuildRequest) (*Build, error) {
	var out Build
	if err := c.do(ctx, http.MethodPost, "/v1/builds", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetBuild(ctx context.Context, id string) (*Build, error) {
	var out Build
	if err := c.do(ctx, http.MethodGet, "/v1/builds/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BuildLogs(ctx context.Context, id string, limit int) ([]BuildLog, error) {
	path := "/v1/builds/" + id + "/logs"
	if limit > 0 {
		path += "?" + url.Values{"limit": {strconv.Itoa(limit)}}.Encode()
	}
	var out struct {
		Logs []BuildLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Logs == nil {
		return []BuildLog{}, nil
	}
	return out.Logs, nil
}

func buildTerminal(status string) bool {
	switch status {
	case "completed", "failed_clone", "failed_build", "failed_push", "failed", "canceled":
		return true
	}
	return false
}

// WaitBuild polls until the build reaches a terminal status.
func (c *Client) WaitBuild(ctx context.Context, id string, poll time.Duration) (*Build, error) {
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	var last *Build
	for {
		b, err := c.GetBuild(ctx, id)
		if err != nil {
			return last, err
		}
		last = b
		if buildTerminal(b.Status) {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(poll):
		}
	}
}
