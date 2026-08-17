package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/knot-infra/knot/pkg/apierrors"
)

// Storage file lifecycle (CP metadata).
const (
	FileUploading  = "uploading"
	FileIncomplete = "incomplete"
	FileComplete   = "complete"
)

type StorageEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_directory"`
	Size     int64  `json:"size,omitempty"`
	Mtime    string `json:"mtime,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileID   string `json:"file_id,omitempty"`
}

type StorageStat struct {
	FileID   string `json:"file_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_directory"`
	Size     int64  `json:"size"`
	Mtime    string `json:"mtime"`
	Mode     string `json:"mode,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type StorageFile struct {
	ID            string `json:"id"`
	DeviceID      string `json:"device_id"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	Status        string `json:"status"`
	TransferID    string `json:"transfer_id"`
	BytesReceived int64  `json:"bytes_received"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// StorageUploadRequest is the Stage 5.0 upload contract.
type StorageUploadRequest struct {
	DeviceID     string
	Path         string
	FromDeviceID string
	SourcePath   string
	Size         int64
	SHA256       string
	Resume       bool
}

func (c *Client) StorageList(ctx context.Context, deviceID, path string) ([]StorageEntry, error) {
	q := url.Values{"device_id": {deviceID}}
	if path != "" {
		q.Set("path", path)
	}
	var out struct {
		Entries []StorageEntry `json:"entries"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/storage/list?"+q.Encode(), nil, &out, true); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func (c *Client) StorageStat(ctx context.Context, deviceID, path string) (*StorageStat, error) {
	q := url.Values{"device_id": {deviceID}, "path": {path}}
	var out StorageStat
	if err := c.do(ctx, http.MethodGet, "/v1/storage/stat?"+q.Encode(), nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageMkdir(ctx context.Context, deviceID, path string) (*StorageStat, error) {
	var out StorageStat
	if err := c.do(ctx, http.MethodPost, "/v1/storage/mkdir", map[string]any{
		"device_id": deviceID, "path": path,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageDelete(ctx context.Context, deviceID, path string) error {
	q := url.Values{"device_id": {deviceID}, "path": {path}}
	return c.do(ctx, http.MethodDelete, "/v1/storage/object?"+q.Encode(), nil, &map[string]any{}, true)
}

func (c *Client) StorageUpload(ctx context.Context, deviceID, path, fromDeviceID, sourcePath string, size int64, sha256 string) (*Transfer, error) {
	return c.StorageUploadOpts(ctx, deviceID, path, fromDeviceID, sourcePath, size, sha256, false)
}

func (c *Client) StorageUploadResume(ctx context.Context, deviceID, path, fromDeviceID, sourcePath string, size int64, sha256 string) (*Transfer, error) {
	return c.StorageUploadOpts(ctx, deviceID, path, fromDeviceID, sourcePath, size, sha256, true)
}

func (c *Client) StorageUploadOpts(ctx context.Context, deviceID, path, fromDeviceID, sourcePath string, size int64, sha256 string, resume bool) (*Transfer, error) {
	return c.StorageUploadRequest(ctx, StorageUploadRequest{
		DeviceID: deviceID, Path: path, FromDeviceID: fromDeviceID,
		SourcePath: sourcePath, Size: size, SHA256: sha256, Resume: resume,
	})
}

func (c *Client) StorageUploadRequest(ctx context.Context, req StorageUploadRequest) (*Transfer, error) {
	var out Transfer
	if err := c.do(ctx, http.MethodPost, "/v1/storage/upload", map[string]any{
		"device_id": req.DeviceID, "path": req.Path,
		"from_device_id": req.FromDeviceID, "source_path": req.SourcePath,
		"size": req.Size, "sha256": req.SHA256, "resume": req.Resume,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageGetFile(ctx context.Context, fileID string) (*StorageFile, error) {
	var out StorageFile
	if err := c.do(ctx, http.MethodGet, "/v1/storage/files/"+fileID, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageMove(ctx context.Context, deviceID, from, to string) (*StorageStat, error) {
	var out StorageStat
	if err := c.do(ctx, http.MethodPost, "/v1/storage/move", map[string]any{
		"device_id": deviceID, "from_path": from, "to_path": to,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageCopy(ctx context.Context, deviceID, from, to string) (*StorageStat, error) {
	var out StorageStat
	if err := c.do(ctx, http.MethodPost, "/v1/storage/copy", map[string]any{
		"device_id": deviceID, "from_path": from, "to_path": to,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

// StorageTransfer moves/copies a storage object between nodes (or same-node copy).
// Cross-node returns a Transfer; same-node completes immediately (Mode=copy).
func (c *Client) StorageTransfer(ctx context.Context, fromDeviceID, fromPath, toDeviceID, toPath string) (*Transfer, error) {
	var out Transfer
	if err := c.do(ctx, http.MethodPost, "/v1/storage/transfer", map[string]any{
		"from_device_id": fromDeviceID, "from_path": fromPath,
		"to_device_id": toDeviceID, "to_path": toPath,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageRead(ctx context.Context, deviceID, path, toDeviceID string) (*Transfer, error) {
	q := url.Values{
		"device_id": {deviceID}, "path": {path}, "to_device_id": {toDeviceID},
	}
	var out Transfer
	if err := c.do(ctx, http.MethodGet, "/v1/storage/read?"+q.Encode(), nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

// StoragePut uploads raw bytes to a node (browser/desktop local files).
func (c *Client) StoragePut(ctx context.Context, deviceID, path, sha256 string, size int64, body io.Reader, opts StoragePutOpts) (*StorageStat, error) {
	q := url.Values{
		"device_id": {deviceID}, "path": {path}, "sha256": {sha256},
	}
	if opts.Overwrite {
		q.Set("overwrite", "true")
	}
	if opts.Conflict != "" {
		q.Set("conflict", opts.Conflict)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/v1/storage/content?"+q.Encode(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		code, msg := apierrors.ParseBody(data)
		if msg == "" {
			msg = string(data)
		}
		return nil, &APIError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	var out StorageStat
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

type StoragePutOpts struct {
	Overwrite bool
	Conflict  string // overwrite | rename
}

// StorageContent downloads small file bytes for preview (max ~8 MiB).
func (c *Client) StorageContent(ctx context.Context, deviceID, path string) (data []byte, contentType string, err error) {
	q := url.Values{"device_id": {deviceID}, "path": {path}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/storage/content?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		code, msg := apierrors.ParseBody(data)
		if msg == "" {
			msg = string(data)
		}
		return nil, "", &APIError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) StoragePreview(ctx context.Context, deviceID, path, variant string, maxPixels int) (data []byte, contentType string, err error) {
	q := url.Values{"device_id": {deviceID}, "path": {path}}
	if variant != "" {
		q.Set("variant", variant)
	}
	if maxPixels > 0 {
		q.Set("max_pixels", fmt.Sprintf("%d", maxPixels))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/storage/preview?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		code, msg := apierrors.ParseBody(data)
		if msg == "" {
			msg = string(data)
		}
		return nil, "", &APIError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	return data, resp.Header.Get("Content-Type"), nil
}
