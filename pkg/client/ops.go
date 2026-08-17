package client

import (
	"context"
	"net/http"
	"net/url"
)

type OpsRelease struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type OpsTraffic struct {
	Hostname        string `json:"hostname"`
	RouteID         string `json:"route_id"`
	ActiveReleaseID string `json:"active_release_id"`
	PrevReleaseID   string `json:"prev_release_id"`
	Weight          int    `json:"weight"`
}

type OpsDeploy struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	Status    string `json:"status"`
	HealthOK  bool   `json:"health_ok"`
	CreatedAt string `json:"created_at"`
}

type OpsContext struct {
	Service        string         `json:"service"`
	ServiceID      string         `json:"service_id,omitempty"`
	Node           string         `json:"node,omitempty"`
	NodeID         string         `json:"node_id,omitempty"`
	Status         string         `json:"status"`
	Environment    string         `json:"environment,omitempty"`
	CurrentRelease *OpsRelease    `json:"current_release,omitempty"`
	LatestRelease  *OpsRelease    `json:"latest_release,omitempty"`
	Traffic        *OpsTraffic    `json:"traffic,omitempty"`
	LastDeploy     *OpsDeploy     `json:"last_deploy,omitempty"`
	LastDeployAt   string         `json:"last_deploy_at,omitempty"`
	RecentErrors   int            `json:"recent_errors"`
	Health         map[string]any `json:"health,omitempty"`
	TraceID        string         `json:"trace_id,omitempty"`
	Visible        []string       `json:"visible"`
	Summary        string         `json:"summary"`
}

func (c *Client) OpsContext(ctx context.Context, service, deviceID string) (*OpsContext, error) {
	q := url.Values{"service": {service}}
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	var out OpsContext
	if err := c.do(ctx, http.MethodGet, "/v1/ops/context?"+q.Encode(), nil, &out, true); err != nil {
		return nil, err
	}
	if out.Visible == nil {
		out.Visible = []string{}
	}
	return &out, nil
}
