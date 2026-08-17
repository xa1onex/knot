package client

import (
	"context"
	"net/http"
)

type Device struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Hostname   string  `json:"hostname"`
	OS         string  `json:"os"`
	Arch       string  `json:"arch"`
	CPUs       int     `json:"cpus"`
	RAMMB        uint64  `json:"ram_mb"`
	AgentVersion string  `json:"agent_version,omitempty"`
	Online       bool    `json:"online"`
	RevokedAt  *string `json:"revoked_at"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
}

type Overview struct {
	DevicesTotal   int `json:"devices_total"`
	DevicesOnline  int `json:"devices_online"`
	DevicesOffline int `json:"devices_offline"`
}

func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	var out struct {
		Devices []Device `json:"devices"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/devices", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func (c *Client) GetDevice(ctx context.Context, id string) (*Device, error) {
	var out Device
	if err := c.do(ctx, http.MethodGet, "/v1/devices/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/devices/"+id+"/revoke", map[string]any{}, &map[string]any{}, true)
}

func (c *Client) CreateRegToken(ctx context.Context, nameHint string, ttlHours int) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/devices/registration-tokens", map[string]any{
		"name_hint": nameHint, "ttl_hours": ttlHours,
	}, &out, true); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (c *Client) Overview(ctx context.Context) (*Overview, error) {
	var out Overview
	if err := c.do(ctx, http.MethodGet, "/v1/overview", nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
