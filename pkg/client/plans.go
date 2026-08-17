package client

import (
	"context"
	"net/http"
)

type PlanCatalogEntry struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Intent   string   `json:"intent"`
	Steps    []string `json:"steps"`
	Risk     string   `json:"risk_level"`
	Approval bool     `json:"requires_approval"`
}

type PlanStep struct {
	ID         string         `json:"id"`
	Seq        int            `json:"seq"`
	Name       string         `json:"name"`
	Title      string         `json:"title"`
	Status     string         `json:"status"`
	Scope      string         `json:"scope"`
	RiskLevel  string         `json:"risk_level"`
	Error      string         `json:"error"`
	Output     map[string]any `json:"output"`
	TraceID    string         `json:"trace_id"`
	StartedAt  string         `json:"started_at,omitempty"`
	FinishedAt string         `json:"finished_at,omitempty"`
}

type Plan struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Title            string         `json:"title"`
	Intent           string         `json:"intent"`
	CreatedBy        string         `json:"created_by"`
	AISessionID      string         `json:"ai_session_id"`
	Actor            string         `json:"actor"`
	TraceID          string         `json:"trace_id"`
	RiskLevel        string         `json:"risk_level"`
	Status           string         `json:"status"`
	RequiresApproval bool           `json:"requires_approval"`
	Error            string         `json:"error"`
	Result           map[string]any `json:"result"`
	Input            map[string]any `json:"input"`
	ApprovedBy       string         `json:"approved_by"`
	ApprovedAt       string         `json:"approved_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	ExpiresAt        string         `json:"expires_at"`
	FinishedAt       string         `json:"finished_at,omitempty"`
	Steps            []PlanStep     `json:"steps,omitempty"`
}

type PlanList struct {
	Catalog []PlanCatalogEntry `json:"catalog"`
	Plans   []Plan             `json:"plans"`
}

type CreatePlanRequest struct {
	Intent       string `json:"intent,omitempty"`
	Name         string `json:"name,omitempty"`
	Service      string `json:"service,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
	Image        string `json:"image,omitempty"`
	BuildID      string `json:"build_id,omitempty"`
	Port         int    `json:"port,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Query        string `json:"query,omitempty"`
	Path         string `json:"path,omitempty"`
	FromDeviceID string `json:"from_device_id,omitempty"`
	ToDeviceID   string `json:"to_device_id,omitempty"`
	ToPath       string `json:"to_path,omitempty"`
	JobImage     string `json:"job_image,omitempty"`
	TTLMinutes   int    `json:"ttl_minutes,omitempty"`
	ExpiresIn    string `json:"expires_in,omitempty"`
	AutoExecute  bool   `json:"auto_execute,omitempty"`
}

func (c *Client) ListPlans(ctx context.Context) (*PlanList, error) {
	var out PlanList
	if err := c.do(ctx, http.MethodGet, "/v1/plans", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Catalog == nil {
		out.Catalog = []PlanCatalogEntry{}
	}
	if out.Plans == nil {
		out.Plans = []Plan{}
	}
	return &out, nil
}

func (c *Client) CreatePlan(ctx context.Context, req CreatePlanRequest) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, http.MethodPost, "/v1/plans", req, &out, true); err != nil {
		return nil, err
	}
	if out.Steps == nil {
		out.Steps = []PlanStep{}
	}
	return &out, nil
}

func (c *Client) GetPlan(ctx context.Context, id string) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, http.MethodGet, "/v1/plans/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Steps == nil {
		out.Steps = []PlanStep{}
	}
	return &out, nil
}

func (c *Client) ApprovePlan(ctx context.Context, id string) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, http.MethodPost, "/v1/plans/"+id+"/approve", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExecutePlan(ctx context.Context, id string) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, http.MethodPost, "/v1/plans/"+id+"/execute", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelPlan(ctx context.Context, id string) (*Plan, error) {
	var out Plan
	if err := c.do(ctx, http.MethodPost, "/v1/plans/"+id+"/cancel", map[string]any{}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
