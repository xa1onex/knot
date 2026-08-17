// Package client is the Stage 5.0 Node Client SDK (Go).
//
// Shells (CLI, MCP, Web) talk only to the Node
// Public API through this contract — never invent their own transfer protocol.
//
// See docs/client-sdk.md and docs/openapi/v1.yaml.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/knot-infra/knot/pkg/apierrors"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	MCPClient  string
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:    trimTrailingSlash(baseURL),
		Token:      token,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// WithToken returns a shallow copy using a different bearer token (e.g. API credential).
func (c *Client) WithToken(token string) *Client {
	cp := *c
	cp.Token = token
	return &cp
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, auth bool) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
		if err != nil {
			return err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth && c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		if tid := TraceIDFrom(ctx); tid != "" {
			req.Header.Set("X-Knot-Trace", tid)
		}
		if name := MCPClientFrom(ctx); name != "" {
			req.Header.Set("X-Knot-MCP-Client", name)
		} else if c.MCPClient != "" {
			req.Header.Set("X-Knot-MCP-Client", c.MCPClient)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode != http.StatusInsufficientStorage) {
			code, msg := apierrors.ParseBody(data)
			if msg == "" {
				msg = string(data)
			}
			lastErr = &APIError{Status: resp.StatusCode, Code: code, Message: msg}
			if code == "account_locked" {
				return lastErr
			}
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 300 {
			code, msg := apierrors.ParseBody(data)
			if msg == "" {
				msg = string(data)
			}
			return &APIError{Status: resp.StatusCode, Code: code, Message: msg}
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return err
			}
		}
		return nil
	}
	return lastErr
}

// Healthz hits GET /healthz (no auth).
func (c *Client) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Readyz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Backup(ctx context.Context) (string, error) {
	var out struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/ops/backup", map[string]any{}, &out, true); err != nil {
		return "", err
	}
	return out.Path, nil
}
