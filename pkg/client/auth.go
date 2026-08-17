package client

import (
	"context"
	"net/http"
)

type LoginResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Token        string `json:"token"`
	User         User   `json:"user"`
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Identity is GET /v1/auth/me.
type Identity struct {
	Kind   string   `json:"kind"`
	UserID string   `json:"user_id"`
	Email  string   `json:"email"`
	Actor  string   `json:"actor"`
	Scopes []string `json:"scopes"`
}

func (c *Client) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	var out LoginResult
	if err := c.do(ctx, http.MethodPost, "/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, &out, false); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		out.AccessToken = out.Token
	}
	c.Token = out.AccessToken
	return &out, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*LoginResult, error) {
	var out LoginResult
	if err := c.do(ctx, http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, &out, false); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		out.AccessToken = out.Token
	}
	c.Token = out.AccessToken
	return &out, nil
}

func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/auth/logout", map[string]any{}, &map[string]any{}, true)
}

func (c *Client) LogoutAll(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/auth/logout-all", map[string]any{}, &map[string]any{}, true)
}

func (c *Client) Me(ctx context.Context) (*Identity, error) {
	var out Identity
	if err := c.do(ctx, http.MethodGet, "/v1/auth/me", nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
