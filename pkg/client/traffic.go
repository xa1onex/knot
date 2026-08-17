package client

import (
	"context"
	"net/http"
)

type RouteTraffic struct {
	RouteID         string               `json:"route_id"`
	Hostname        string               `json:"hostname"`
	ServiceID       string               `json:"service_id"`
	Service         string               `json:"service"`
	TLSMode         string               `json:"tls_mode"`
	ActiveReleaseID string               `json:"active_release_id"`
	PrevReleaseID   string               `json:"prev_release_id"`
	Targets         []RouteTrafficTarget `json:"targets"`
	History         []RouteTrafficEvent  `json:"history"`
}

type RouteTrafficTarget struct {
	ReleaseID string `json:"release_id"`
	Number    int    `json:"number"`
	Image     string `json:"image"`
	Status    string `json:"status"`
	Weight    int    `json:"weight"`
	Current   bool   `json:"current"`
	Port      int    `json:"port"`
	DeviceID  string `json:"device_id"`
}

type RouteTrafficEvent struct {
	ID            string         `json:"id"`
	Action        string         `json:"action"`
	FromReleaseID string         `json:"from_release_id"`
	ToReleaseID   string         `json:"to_release_id"`
	Weights       map[string]int `json:"weights"`
	CreatedBy     string         `json:"created_by"`
	CreatedAt     string         `json:"created_at"`
}

type SwitchTrafficRequest struct {
	ReleaseID string `json:"release_id"`
	Weight    int    `json:"weight,omitempty"`
}

func (c *Client) GetRouteTraffic(ctx context.Context, idOrHost string) (*RouteTraffic, error) {
	var out RouteTraffic
	if err := c.do(ctx, http.MethodGet, "/v1/routes/"+idOrHost+"/traffic", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Targets == nil {
		out.Targets = []RouteTrafficTarget{}
	}
	if out.History == nil {
		out.History = []RouteTrafficEvent{}
	}
	return &out, nil
}

func (c *Client) SwitchRouteTraffic(ctx context.Context, idOrHost, releaseID string, weight int) (*RouteTraffic, error) {
	var out RouteTraffic
	if err := c.do(ctx, http.MethodPost, "/v1/routes/"+idOrHost+"/switch", SwitchTrafficRequest{
		ReleaseID: releaseID, Weight: weight,
	}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RollbackRouteTraffic(ctx context.Context, idOrHost string) (*RouteTraffic, error) {
	var out RouteTraffic
	if err := c.do(ctx, http.MethodPost, "/v1/routes/"+idOrHost+"/rollback", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
