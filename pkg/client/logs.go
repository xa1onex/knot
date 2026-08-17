package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type OpsLog struct {
	ID           string         `json:"id"`
	Timestamp    string         `json:"timestamp"`
	Level        string         `json:"level"`
	Source       string         `json:"source"`
	Message      string         `json:"message"`
	TraceID      string         `json:"trace_id"`
	DeviceID     string         `json:"device_id"`
	ServiceID    string         `json:"service_id"`
	Service      string         `json:"service"`
	ReleaseID    string         `json:"release_id"`
	BuildID      string         `json:"build_id"`
	JobID        string         `json:"job_id"`
	DeploymentID string         `json:"deployment_id"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type ListLogsQuery struct {
	Service      string
	ServiceID    string
	ReleaseID    string
	BuildID      string
	JobID        string
	DeploymentID string
	Source       string
	TraceID      string
	Level        string
	Q            string
	After        string
	Since        string
	Until        string
	Limit        int
}

type IngestLogRequest struct {
	Level        string         `json:"level,omitempty"`
	Source       string         `json:"source,omitempty"`
	Message      string         `json:"message"`
	TraceID      string         `json:"trace_id,omitempty"`
	DeviceID     string         `json:"device_id,omitempty"`
	ServiceID    string         `json:"service_id,omitempty"`
	Service      string         `json:"service,omitempty"`
	ReleaseID    string         `json:"release_id,omitempty"`
	BuildID      string         `json:"build_id,omitempty"`
	JobID        string         `json:"job_id,omitempty"`
	DeploymentID string         `json:"deployment_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

func (c *Client) ListLogs(ctx context.Context, q ListLogsQuery) ([]OpsLog, error) {
	var out struct {
		Logs []OpsLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/logs"+encodeLogsQuery(q), nil, &out, true); err != nil {
		return nil, err
	}
	if out.Logs == nil {
		return []OpsLog{}, nil
	}
	return out.Logs, nil
}

func (c *Client) IngestLog(ctx context.Context, req IngestLogRequest) (*OpsLog, error) {
	var out OpsLog
	if err := c.do(ctx, http.MethodPost, "/v1/logs", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func encodeLogsQuery(q ListLogsQuery) string {
	v := url.Values{}
	if q.Service != "" {
		v.Set("service", q.Service)
	}
	if q.ServiceID != "" {
		v.Set("service_id", q.ServiceID)
	}
	if q.ReleaseID != "" {
		v.Set("release_id", q.ReleaseID)
	}
	if q.BuildID != "" {
		v.Set("build_id", q.BuildID)
	}
	if q.JobID != "" {
		v.Set("job_id", q.JobID)
	}
	if q.DeploymentID != "" {
		v.Set("deployment_id", q.DeploymentID)
	}
	if q.Source != "" {
		v.Set("source", q.Source)
	}
	if q.TraceID != "" {
		v.Set("trace_id", q.TraceID)
	}
	if q.Level != "" {
		v.Set("level", q.Level)
	}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.After != "" {
		v.Set("after", q.After)
	}
	if q.Since != "" {
		v.Set("since", q.Since)
	}
	if q.Until != "" {
		v.Set("until", q.Until)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	enc := v.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}
