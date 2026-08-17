package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type Credential struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	TokenPrefix string   `json:"token_prefix"`
	Scopes      []string `json:"scopes"`
	ExpiresAt   *string  `json:"expires_at"`
	RevokedAt   *string  `json:"revoked_at"`
	CreatedAt   string   `json:"created_at"`
	Token       string   `json:"token,omitempty"` // only on create/rotate
}

type AuditEvent struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	ActorType   string `json:"actor_type"`
	Actor       string `json:"actor"`
	ActorID     string `json:"actor_id"`
	Parent      string `json:"parent"`
	ParentActor string `json:"parent_actor"`
	AISessionID string `json:"ai_session_id"`
	MCPClient   string `json:"mcp_client"`
	WorkflowID  string `json:"workflow_id"`
	TraceID     string `json:"trace_id"`
	Action      string `json:"action"`
	Resource    string `json:"resource"`
	Target      string `json:"target"`
	Detail      string `json:"detail"`
	Result      string `json:"result"`
	Route       string `json:"route,omitempty"`
	Release     string `json:"release,omitempty"`
	Time        string `json:"time"`
	CreatedAt   string `json:"created_at"`
}

type AuditQuery struct {
	ActorType   string
	ActorID     string
	AISessionID string
	WorkflowID  string
	TraceID     string
	Action      string
	MCPClient   string
	Q           string
	Limit       int
}

type AIActivityStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Status string `json:"status"`
}

type AIActivity struct {
	Time       string           `json:"time"`
	ActorType  string           `json:"actor_type"`
	Actor      string           `json:"actor"`
	Parent     string           `json:"parent"`
	SessionID  string           `json:"ai_session_id"`
	MCPClient  string           `json:"mcp_client"`
	WorkflowID string           `json:"workflow_id"`
	TraceID    string           `json:"trace_id"`
	Ran        string           `json:"ran"`
	Service    string           `json:"service"`
	Target     string           `json:"target"`
	Steps      []AIActivityStep `json:"steps"`
	Result     string           `json:"result"`
	Action     string           `json:"action"`
}

func (c *Client) ListCredentials(ctx context.Context) ([]Credential, error) {
	var out struct {
		Credentials []Credential `json:"credentials"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/credentials", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Credentials, nil
}

func (c *Client) CreateCredential(ctx context.Context, name string, scopes []string, ttlDays int) (id, token string, err error) {
	var out Credential
	if err := c.do(ctx, http.MethodPost, "/v1/credentials", map[string]any{
		"name": name, "scopes": scopes, "ttl_days": ttlDays,
	}, &out, true); err != nil {
		return "", "", err
	}
	return out.ID, out.Token, nil
}

func (c *Client) RotateCredential(ctx context.Context, id string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/credentials/"+id+"/rotate", map[string]any{}, &out, true); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (c *Client) RevokeCredential(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/credentials/"+id+"/revoke", map[string]any{}, &map[string]any{}, true)
}

func (c *Client) ListActivity(ctx context.Context) ([]AuditEvent, error) {
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/activity", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Events, nil
}

func (c *Client) SearchAudit(ctx context.Context, q AuditQuery) ([]AuditEvent, error) {
	v := url.Values{}
	if q.ActorType != "" {
		v.Set("actor_type", q.ActorType)
	}
	if q.ActorID != "" {
		v.Set("actor_id", q.ActorID)
	}
	if q.AISessionID != "" {
		v.Set("ai_session_id", q.AISessionID)
	}
	if q.WorkflowID != "" {
		v.Set("workflow_id", q.WorkflowID)
	}
	if q.TraceID != "" {
		v.Set("trace_id", q.TraceID)
	}
	if q.Action != "" {
		v.Set("action", q.Action)
	}
	if q.MCPClient != "" {
		v.Set("mcp_client", q.MCPClient)
	}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	path := "/v1/audit"
	if enc := v.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	return out.Events, nil
}

func (c *Client) AIActivity(ctx context.Context, q AuditQuery) ([]AIActivity, error) {
	v := url.Values{}
	if q.AISessionID != "" {
		v.Set("ai_session_id", q.AISessionID)
	}
	if q.WorkflowID != "" {
		v.Set("workflow_id", q.WorkflowID)
	}
	if q.TraceID != "" {
		v.Set("trace_id", q.TraceID)
	}
	if q.MCPClient != "" {
		v.Set("mcp_client", q.MCPClient)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	path := "/v1/audit/ai"
	if enc := v.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Activities []AIActivity `json:"activities"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true); err != nil {
		return nil, err
	}
	return out.Activities, nil
}

func (c *Client) AuditTrace(ctx context.Context, traceID string) ([]AuditEvent, error) {
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/audit/trace/"+url.PathEscape(traceID), nil, &out, true); err != nil {
		return nil, err
	}
	return out.Events, nil
}
