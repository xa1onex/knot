package client

import (
	"context"
	"net/http"
)

type WorkflowCatalogEntry struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Steps    []string `json:"steps"`
	Mutating bool     `json:"mutating"`
}

type WorkflowStep struct {
	ID         string         `json:"id"`
	Seq        int            `json:"seq"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Scope      string         `json:"scope"`
	Error      string         `json:"error"`
	Output     map[string]any `json:"output"`
	TraceID    string         `json:"trace_id"`
	StartedAt  string         `json:"started_at,omitempty"`
	FinishedAt string         `json:"finished_at,omitempty"`
}

type Workflow struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Title      string         `json:"title"`
	Actor      string         `json:"actor"`
	Status     string         `json:"status"`
	TraceID    string         `json:"trace_id"`
	Error      string         `json:"error"`
	Result     map[string]any `json:"result"`
	Steps      []WorkflowStep `json:"steps,omitempty"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
	FinishedAt string         `json:"finished_at,omitempty"`
}

type WorkflowList struct {
	Catalog   []WorkflowCatalogEntry `json:"catalog"`
	Workflows []Workflow             `json:"workflows"`
}

type RunWorkflowRequest struct {
	Name         string `json:"name"`
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
}

func (c *Client) ListWorkflows(ctx context.Context) (*WorkflowList, error) {
	var out WorkflowList
	if err := c.do(ctx, http.MethodGet, "/v1/workflows", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Catalog == nil {
		out.Catalog = []WorkflowCatalogEntry{}
	}
	if out.Workflows == nil {
		out.Workflows = []Workflow{}
	}
	return &out, nil
}

func (c *Client) RunWorkflow(ctx context.Context, req RunWorkflowRequest) (*Workflow, error) {
	var out Workflow
	if err := c.do(ctx, http.MethodPost, "/v1/workflows/run", req, &out, true); err != nil {
		return nil, err
	}
	if out.Steps == nil {
		out.Steps = []WorkflowStep{}
	}
	if out.Result == nil {
		out.Result = map[string]any{}
	}
	return &out, nil
}

func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var out Workflow
	if err := c.do(ctx, http.MethodGet, "/v1/workflows/"+id, nil, &out, true); err != nil {
		return nil, err
	}
	if out.Steps == nil {
		out.Steps = []WorkflowStep{}
	}
	if out.Result == nil {
		out.Result = map[string]any{}
	}
	return &out, nil
}

func (c *Client) WorkflowSteps(ctx context.Context, id string) ([]WorkflowStep, error) {
	var out struct {
		Steps []WorkflowStep `json:"steps"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/workflows/"+id+"/steps", nil, &out, true); err != nil {
		return nil, err
	}
	if out.Steps == nil {
		return []WorkflowStep{}, nil
	}
	return out.Steps, nil
}
