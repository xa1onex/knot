package client

import (
	"context"
	"net/http"
	"time"
)

// Transfer statuses (contract).
const (
	TransferPending      = "pending"
	TransferOffered      = "offered"
	TransferNegotiating  = "negotiating"
	TransferTransferring = "transferring"
	TransferCompleted    = "completed"
	TransferFailed       = "failed"
	TransferAborted      = "aborted"
)

type Transfer struct {
	ID            string  `json:"id"`
	FromDeviceID  string  `json:"from_device_id"`
	ToDeviceID    string  `json:"to_device_id"`
	Filename      string  `json:"filename"`
	SourcePath    string  `json:"source_path"`
	Size          int64   `json:"size"`
	SHA256        string  `json:"sha256"`
	Status        string  `json:"status"`
	Error         string  `json:"error"`
	Path          string  `json:"path"` // transport: direct|relay
	FileID        string  `json:"file_id"`
	ResumeOffset  int64   `json:"resume_offset"`
	BytesReceived int64   `json:"bytes_received"`
	FileStatus    string  `json:"file_status,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	CompletedAt   *string `json:"completed_at"`
}

// Progress is the client-facing transfer progress snapshot.
type Progress struct {
	TransferID    string  `json:"transfer_id"`
	Status        string  `json:"status"`
	BytesReceived int64   `json:"bytes_received"`
	Size          int64   `json:"size"`
	Percent       float64 `json:"percent"` // 0..100
	Path          string  `json:"path,omitempty"`
	FileID        string  `json:"file_id,omitempty"`
	Error         string  `json:"error,omitempty"`
}

func ProgressFromTransfer(t *Transfer) Progress {
	p := Progress{
		TransferID: t.ID, Status: t.Status, Size: t.Size,
		Path: t.Path, FileID: t.FileID, Error: t.Error,
	}
	p.BytesReceived = t.BytesReceived
	if p.BytesReceived == 0 {
		p.BytesReceived = t.ResumeOffset
	}
	if t.Status == TransferCompleted {
		p.BytesReceived = t.Size
		p.Percent = 100
	} else if t.Size > 0 {
		p.Percent = float64(p.BytesReceived) * 100 / float64(t.Size)
		if p.Percent > 100 {
			p.Percent = 100
		}
	}
	return p
}

func (t *Transfer) Terminal() bool {
	switch t.Status {
	case TransferCompleted, TransferFailed, TransferAborted:
		return true
	default:
		return false
	}
}

func (c *Client) CreateTransfer(ctx context.Context, fromID, toID, filename, sourcePath string, size int64, sha256 string) (*Transfer, error) {
	var out Transfer
	if err := c.do(ctx, http.MethodPost, "/v1/transfers", map[string]any{
		"from_device_id": fromID,
		"to_device_id":   toID,
		"filename":       filename,
		"source_path":    sourcePath,
		"size":           size,
		"sha256":         sha256,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTransfer(ctx context.Context, id string) (*Transfer, error) {
	var out Transfer
	if err := c.do(ctx, http.MethodGet, "/v1/transfers/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListTransfers(ctx context.Context) ([]Transfer, error) {
	var out struct {
		Transfers []Transfer `json:"transfers"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/transfers", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Transfers, nil
}

func (c *Client) AbortTransfer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/transfers/"+id+"/abort", map[string]any{}, &map[string]any{}, true)
}

// WaitTransfer blocks until the transfer reaches a terminal status.
func (c *Client) WaitTransfer(ctx context.Context, id string, poll time.Duration) (*Transfer, error) {
	return c.WatchTransfer(ctx, id, poll, nil)
}

// WatchTransfer polls until terminal, invoking onProgress on each snapshot (may be nil).
func (c *Client) WatchTransfer(ctx context.Context, id string, poll time.Duration, onProgress func(Progress)) (*Transfer, error) {
	if poll <= 0 {
		poll = 300 * time.Millisecond
	}
	var lastBytes int64 = -1
	var lastStatus string
	for {
		t, err := c.GetTransfer(ctx, id)
		if err != nil {
			return nil, err
		}
		p := ProgressFromTransfer(t)
		if onProgress != nil && (p.BytesReceived != lastBytes || t.Status != lastStatus) {
			onProgress(p)
			lastBytes = p.BytesReceived
			lastStatus = t.Status
		}
		if t.Terminal() {
			return t, nil
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(poll):
		}
	}
}
