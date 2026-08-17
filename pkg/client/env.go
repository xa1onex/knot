package client

import (
	"context"
	"net/http"
	"net/url"
)

type Secret struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (c *Client) ListSecrets(ctx context.Context) ([]Secret, error) {
	var out struct {
		Secrets []Secret `json:"secrets"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/secrets", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Secrets == nil {
		return []Secret{}, nil
	}
	return out.Secrets, nil
}

func (c *Client) CreateSecret(ctx context.Context, name, value string) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodPost, "/v1/secrets", map[string]string{"name": name, "value": value}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSecret(ctx context.Context, id string) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodGet, "/v1/secrets/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RotateSecret(ctx context.Context, id, value string) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodPut, "/v1/secrets/"+id, map[string]string{"value": value}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

type EnvironmentSecretRef struct {
	Key      string `json:"key"`
	SecretID string `json:"secret_id"`
	Name     string `json:"name"`
	Version  int    `json:"version"`
}

type Environment struct {
	ID        string                 `json:"id"`
	Project   string                 `json:"project"`
	Name      string                 `json:"name"`
	Vars      map[string]string      `json:"vars"`
	Secrets   []EnvironmentSecretRef `json:"secrets"`
	Policy    map[string]string      `json:"policy"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

type CreateEnvironmentRequest struct {
	Project string            `json:"project,omitempty"`
	Name    string            `json:"name"`
	Vars    map[string]string `json:"vars,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
	Policy  map[string]string `json:"policy,omitempty"`
}

func (c *Client) ListEnvironments(ctx context.Context, project string) ([]Environment, error) {
	path := "/v1/environments"
	if project != "" {
		path += "?" + url.Values{"project": {project}}.Encode()
	}
	var out struct {
		Environments []Environment `json:"environments"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Environments == nil {
		return []Environment{}, nil
	}
	return out.Environments, nil
}

func (c *Client) CreateEnvironment(ctx context.Context, req CreateEnvironmentRequest) (*Environment, error) {
	var out Environment
	if err := c.do(ctx, http.MethodPost, "/v1/environments", req, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEnvironment(ctx context.Context, id string) (*Environment, error) {
	var out Environment
	if err := c.do(ctx, http.MethodGet, "/v1/environments/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateEnvironment(ctx context.Context, id string, vars, secretMap, policy map[string]string) (*Environment, error) {
	var out Environment
	body := map[string]any{}
	if vars != nil {
		body["vars"] = vars
	}
	if secretMap != nil {
		body["secrets"] = secretMap
	}
	if policy != nil {
		body["policy"] = policy
	}
	if err := c.do(ctx, http.MethodPut, "/v1/environments/"+id, body, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
