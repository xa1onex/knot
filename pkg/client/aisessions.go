package client

import (
	"context"
	"net/http"
)

type AISession struct {
	ID           string   `json:"id"`
	CredentialID string   `json:"credential_id"`
	Name         string   `json:"name"`
	CreatedBy    string   `json:"created_by"`
	Parent       string   `json:"parent"`
	Actor        string   `json:"actor,omitempty"`
	Scopes       []string `json:"scopes"`
	Status       string   `json:"status"`
	ExpiresAt    string   `json:"expires_at"`
	CreatedAt    string   `json:"created_at"`
	RevokedAt    *string  `json:"revoked_at,omitempty"`
	Token        string   `json:"token,omitempty"`
}

type CreateAISessionRequest struct {
	Name       string   `json:"name,omitempty"`
	Scopes     []string `json:"scopes"`
	TTLMinutes int      `json:"ttl_minutes,omitempty"`
	ExpiresIn  string   `json:"expires_in,omitempty"`
}

func (c *Client) ListAISessions(ctx context.Context) ([]AISession, error) {
	var out struct {
		Sessions []AISession `json:"sessions"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/ai/sessions", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Sessions == nil {
		return []AISession{}, nil
	}
	return out.Sessions, nil
}

func (c *Client) CreateAISession(ctx context.Context, req CreateAISessionRequest) (*AISession, error) {
	var out AISession
	if err := c.do(ctx, http.MethodPost, "/v1/ai/sessions", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetAISession(ctx context.Context, id string) (*AISession, error) {
	var out AISession
	if err := c.do(ctx, http.MethodGet, "/v1/ai/sessions/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CurrentAISession(ctx context.Context) (*AISession, error) {
	var out AISession
	if err := c.do(ctx, http.MethodGet, "/v1/ai/sessions/current", nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RevokeAISession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/ai/sessions/"+id, nil, &map[string]any{}, true)
}
