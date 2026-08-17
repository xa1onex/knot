package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// FileHit is one metadata row from GET /v1/files/search (Stage 6.5).
type FileHit struct {
	ID          string `json:"id"`
	FileID      string `json:"file_id"`
	DeviceID    string `json:"device_id"`
	DeviceName  string `json:"device_name"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Mtime       string `json:"mtime"`
	SHA256      string `json:"sha256"`
	MimeType    string `json:"mime_type"`
	IsDirectory bool   `json:"is_directory"`
	IndexedAt   string `json:"indexed_at"`
}

type FileSearchQuery struct {
	Query          string
	DeviceID       string
	Folder         string
	Type           string
	MinSize        int64
	MaxSize        int64
	ModifiedAfter  string
	ModifiedBefore string
	IsDirectory    *bool
	Limit          int
}

type FilesReindexResult struct {
	DeviceIDs []string `json:"device_ids"`
	Entries   int      `json:"entries"`
	Skipped   []string `json:"skipped,omitempty"`
	Errors    []string `json:"errors,omitempty"`
}

func (c *Client) FilesSearch(ctx context.Context, q FileSearchQuery) ([]FileHit, error) {
	v := url.Values{}
	if q.Query != "" {
		v.Set("q", q.Query)
	}
	if q.DeviceID != "" {
		v.Set("device_id", q.DeviceID)
	}
	if q.Folder != "" {
		v.Set("folder", q.Folder)
	}
	if q.Type != "" {
		v.Set("type", q.Type)
	}
	if q.MinSize > 0 {
		v.Set("min_size", strconv.FormatInt(q.MinSize, 10))
	}
	if q.MaxSize > 0 {
		v.Set("max_size", strconv.FormatInt(q.MaxSize, 10))
	}
	if q.ModifiedAfter != "" {
		v.Set("modified_after", q.ModifiedAfter)
	}
	if q.ModifiedBefore != "" {
		v.Set("modified_before", q.ModifiedBefore)
	}
	if q.IsDirectory != nil {
		if *q.IsDirectory {
			v.Set("is_directory", "true")
		} else {
			v.Set("is_directory", "false")
		}
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out struct {
		Files []FileHit `json:"files"`
	}
	path := "/v1/files/search"
	if enc := v.Encode(); enc != "" {
		path += "?" + enc
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Files == nil {
		return []FileHit{}, nil
	}
	return out.Files, nil
}

func (c *Client) FilesReindex(ctx context.Context, deviceID string) (*FilesReindexResult, error) {
	body := map[string]any{}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	var out FilesReindexResult
	if err := c.do(ctx, http.MethodPost, "/v1/files/reindex", body, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
