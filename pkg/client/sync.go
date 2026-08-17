package client

import (
	"context"
	"net/http"
	"time"
)

type SyncJob struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Mode              string  `json:"mode"`
	SourceDeviceID    string  `json:"source_device_id"`
	SourcePath        string  `json:"source_path"`
	DestDeviceID      string  `json:"dest_device_id"`
	DestPath          string  `json:"dest_path"`
	Status            string  `json:"status"`
	FilesTotal        int64   `json:"files_total"`
	FilesDone         int64   `json:"files_done"`
	BytesTotal        int64   `json:"bytes_total"`
	BytesDone         int64   `json:"bytes_done"`
	CurrentPath       string  `json:"current_path"`
	CurrentTransferID string  `json:"current_transfer_id"`
	ConflictsOpen     int64   `json:"conflicts_open"`
	LastError         string  `json:"last_error"`
	LastRunAt         *string `json:"last_run_at,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type SyncFileState struct {
	RelPath      string  `json:"rel_path"`
	Size         int64   `json:"size"`
	Mtime        string  `json:"mtime"`
	SHA256       string  `json:"sha256"`
	IsDir        bool    `json:"is_dir"`
	Status       string  `json:"status"`
	LastSyncedAt *string `json:"last_synced_at,omitempty"`
}

type CreateSyncJobRequest struct {
	Name           string `json:"name,omitempty"`
	Mode           string `json:"mode,omitempty"` // one_way | two_way
	SourceDeviceID string `json:"source_device_id"`
	SourcePath     string `json:"source_path"`
	DestDeviceID   string `json:"dest_device_id"`
	DestPath       string `json:"dest_path"`
}

func (c *Client) CreateSyncJob(ctx context.Context, req CreateSyncJobRequest) (*SyncJob, error) {
	var out SyncJob
	if err := c.do(ctx, http.MethodPost, "/v1/sync/jobs", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListSyncJobs(ctx context.Context) ([]SyncJob, error) {
	var out struct {
		Jobs []SyncJob `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sync/jobs", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

func (c *Client) GetSyncJob(ctx context.Context, id string) (*SyncJob, error) {
	var out SyncJob
	if err := c.do(ctx, http.MethodGet, "/v1/sync/jobs/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RunSyncJob(ctx context.Context, id string) (*SyncJob, error) {
	var out SyncJob
	if err := c.do(ctx, http.MethodPost, "/v1/sync/jobs/"+id+"/run", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelSyncJob(ctx context.Context, id string) (*SyncJob, error) {
	var out SyncJob
	if err := c.do(ctx, http.MethodPost, "/v1/sync/jobs/"+id+"/cancel", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSyncJob(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/sync/jobs/"+id, nil, &map[string]any{}, true)
}

func (c *Client) ListSyncFiles(ctx context.Context, jobID string) ([]SyncFileState, error) {
	var out struct {
		Files []SyncFileState `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sync/jobs/"+jobID+"/files", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Files, nil
}

func (c *Client) ListSyncConflicts(ctx context.Context, jobID string) ([]SyncConflict, error) {
	return c.ListSyncConflictsOpt(ctx, jobID, true)
}

func (c *Client) ListSyncConflictsOpt(ctx context.Context, jobID string, openOnly bool) ([]SyncConflict, error) {
	path := "/v1/sync/jobs/" + jobID + "/conflicts"
	if !openOnly {
		path += "?open=false"
	}
	var out struct {
		Conflicts []SyncConflict `json:"conflicts"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	return out.Conflicts, nil
}

func (c *Client) ResolveSyncConflict(ctx context.Context, conflictID, resolution string) (*SyncConflict, error) {
	var out SyncConflict
	if err := c.do(ctx, http.MethodPost, "/v1/sync/conflicts/"+conflictID+"/resolve", map[string]string{
		"resolution": resolution,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

type SyncConflict struct {
	ID                    string  `json:"id"`
	JobID                 string  `json:"job_id"`
	RelPath               string  `json:"rel_path"`
	Status                string  `json:"status"`
	AExists               bool    `json:"a_exists"`
	ADeleted              bool    `json:"a_deleted"`
	ASize                 int64   `json:"a_size"`
	AMtime                string  `json:"a_mtime"`
	ASHA256               string  `json:"a_sha256"`
	BExists               bool    `json:"b_exists"`
	BDeleted              bool    `json:"b_deleted"`
	BSize                 int64   `json:"b_size"`
	BMtime                string  `json:"b_mtime"`
	BSHA256               string  `json:"b_sha256"`
	BaseSHA256            string  `json:"base_sha256"`
	BaseSize              int64   `json:"base_size"`
	Resolution            string  `json:"resolution"`
	CreatedAt             string  `json:"created_at"`
	ResolvedAt            *string `json:"resolved_at,omitempty"`
	ADeviceID             string  `json:"a_device_id,omitempty"`
	BDeviceID             string  `json:"b_device_id,omitempty"`
	ADeviceName           string  `json:"a_device_name,omitempty"`
	BDeviceName           string  `json:"b_device_name,omitempty"`
	ARoot                 string  `json:"a_root,omitempty"`
	BRoot                 string  `json:"b_root,omitempty"`
	KeepBothSuggestedName string  `json:"keep_both_suggested_name,omitempty"`
}

func (c *Client) BatchResolveSyncConflicts(ctx context.Context, conflictIDs []string, resolution string) (resolved []SyncConflict, errs []string, err error) {
	var out struct {
		Resolved []SyncConflict `json:"resolved"`
		Errors   []string       `json:"errors"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/sync/conflicts/batch-resolve", map[string]any{
		"conflict_ids": conflictIDs,
		"resolution":   resolution,
	}, &out, true); err != nil {
		return nil, nil, err
	}
	return out.Resolved, out.Errors, nil
}
func (c *Client) WaitSyncJob(ctx context.Context, id string, poll time.Duration) (*SyncJob, error) {
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	for {
		j, err := c.GetSyncJob(ctx, id)
		if err != nil {
			return nil, err
		}
		switch j.Status {
		case "completed", "completed_with_conflicts", "failed", "paused", "idle":
			return j, nil
		}
		select {
		case <-ctx.Done():
			return j, ctx.Err()
		case <-time.After(poll):
		}
	}
}

type SyncFlushResult struct {
	DeviceID      string   `json:"device_id"`
	JobIDs        []string `json:"job_ids"`
	ConflictPaths []string `json:"conflict_paths"`
	Errors        []string `json:"errors,omitempty"`
}

func (c *Client) FlushSync(ctx context.Context, deviceID string) (*SyncFlushResult, error) {
	var out SyncFlushResult
	if err := c.do(ctx, http.MethodPost, "/v1/sync/flush", map[string]string{"device_id": deviceID}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSyncFlushStatus(ctx context.Context, deviceID string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodGet, "/v1/sync/flush/"+deviceID, nil, &out, true); err != nil {
		return nil, err
	}
	return out, nil
}
