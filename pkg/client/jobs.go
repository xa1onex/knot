package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type JobResources struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int64   `json:"memory_mb"`
	GPU      int     `json:"gpu"`
	Pids     int64   `json:"pids,omitempty"`
	DiskMB   int64   `json:"disk_mb,omitempty"`
}

type ComputeJob struct {
	ID             string            `json:"id"`
	JobID          string            `json:"job_id"`
	DeviceID       string            `json:"device_id"`
	DeviceName     string            `json:"device_name"`
	DeviceOnline   bool              `json:"device_online"`
	Image          string            `json:"image"`
	Command        []string          `json:"command"`
	Env            map[string]string `json:"env"`
	Resources      JobResources      `json:"resources"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	InputPath      string            `json:"input_path"`
	OutputPath     string            `json:"output_path"`
	Status         string            `json:"status"`
	Reason         string            `json:"reason"`
	ExitCode       *int              `json:"exit_code"`
	Error          string            `json:"error"`
	ContainerID    string            `json:"container_id"`
	CreatedAt      string            `json:"created_at"`
	StartedAt      *string           `json:"started_at"`
	FinishedAt     *string           `json:"finished_at"`
	Artifacts      []JobArtifact     `json:"artifacts,omitempty"`
	Placement      string            `json:"placement,omitempty"`
	Require        map[string]string `json:"require,omitempty"`
	Prefer         map[string]string `json:"prefer,omitempty"`
	Attempts       int               `json:"attempts,omitempty"`
	MaxRetries     int               `json:"max_retries,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
}

type JobArtifact struct {
	ArtifactID string `json:"artifact_id"`
	JobID      string `json:"job_id"`
	FileID     string `json:"file_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	MimeType   string `json:"mime_type"`
	CreatedAt  string `json:"created_at"`
}

type CreateJobRequest struct {
	DeviceID       string            `json:"device_id"`
	Image          string            `json:"image"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Resources      JobResources      `json:"resources"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	InputPath      string            `json:"input_path,omitempty"`
	OutputPath     string            `json:"output_path,omitempty"`
	Require        map[string]string `json:"require,omitempty"`
	Prefer         map[string]string `json:"prefer,omitempty"`
	RetryMax       *int              `json:"retry_max,omitempty"`
}

type JobLog struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) ListJobs(ctx context.Context, deviceID string) ([]ComputeJob, error) {
	path := "/v1/compute/jobs"
	if deviceID != "" {
		path += "?" + url.Values{"device_id": {deviceID}}.Encode()
	}
	var out struct {
		Jobs []ComputeJob `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Jobs == nil {
		return []ComputeJob{}, nil
	}
	return out.Jobs, nil
}

func (c *Client) CreateJob(ctx context.Context, req CreateJobRequest) (*ComputeJob, error) {
	var out ComputeJob
	if err := c.do(ctx, http.MethodPost, "/v1/compute/jobs", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetJob(ctx context.Context, id string) (*ComputeJob, error) {
	var out ComputeJob
	if err := c.do(ctx, http.MethodGet, "/v1/compute/jobs/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelJob(ctx context.Context, id string) (*ComputeJob, error) {
	var out ComputeJob
	if err := c.do(ctx, http.MethodPost, "/v1/compute/jobs/"+id+"/cancel", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) JobLogs(ctx context.Context, id string, limit int) ([]JobLog, error) {
	path := "/v1/compute/jobs/" + id + "/logs"
	if limit > 0 {
		path += "?" + url.Values{"limit": {strconv.Itoa(limit)}}.Encode()
	}
	var out struct {
		Logs []JobLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Logs == nil {
		return []JobLog{}, nil
	}
	return out.Logs, nil
}

func (c *Client) JobArtifacts(ctx context.Context, id string) ([]JobArtifact, error) {
	var out struct {
		Artifacts []JobArtifact `json:"artifacts"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/compute/jobs/"+id+"/artifacts", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Artifacts == nil {
		return []JobArtifact{}, nil
	}
	return out.Artifacts, nil
}

func jobTerminal(status string) bool {
	switch status {
	case "succeeded", "artifacts_committed", "failed", "timeout", "canceled", "rejected":
		return true
	}
	return false
}

// WaitJob polls until the job reaches a terminal status.
func (c *Client) WaitJob(ctx context.Context, id string, poll time.Duration) (*ComputeJob, error) {
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	var last *ComputeJob
	for {
		j, err := c.GetJob(ctx, id)
		if err != nil {
			return last, err
		}
		last = j
		if jobTerminal(j.Status) {
			return j, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(poll):
		}
	}
}
