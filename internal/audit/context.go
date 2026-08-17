package audit

import (
	"context"
	"strings"

	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
)

type ctxKey int

const metaKey ctxKey = 1

// Meta is filled from the authenticated identity and MCP headers, then
// copied onto every audit_events row. Call sites keep using Logger.Log.
type Meta struct {
	ActorType   string
	ActorID     string
	ActorName   string
	ParentActor string
	AISessionID string
	MCPClient   string
	WorkflowID  string
	PlanID      string
	TraceID     string
}

func WithMeta(ctx context.Context, m Meta) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cur := MetaFrom(ctx)
	if m.ActorType == "" {
		m.ActorType = cur.ActorType
	}
	if m.ActorID == "" {
		m.ActorID = cur.ActorID
	}
	if m.ActorName == "" {
		m.ActorName = cur.ActorName
	}
	if m.ParentActor == "" {
		m.ParentActor = cur.ParentActor
	}
	if m.AISessionID == "" {
		m.AISessionID = cur.AISessionID
	}
	if m.MCPClient == "" {
		m.MCPClient = cur.MCPClient
	}
	if m.WorkflowID == "" {
		m.WorkflowID = cur.WorkflowID
	}
	if m.PlanID == "" {
		m.PlanID = cur.PlanID
	}
	if m.TraceID == "" {
		m.TraceID = cur.TraceID
	}
	return context.WithValue(ctx, metaKey, m)
}

func MetaFrom(ctx context.Context) Meta {
	if ctx == nil {
		return Meta{}
	}
	v, _ := ctx.Value(metaKey).(Meta)
	return v
}

func WithWorkflow(ctx context.Context, workflowID string) context.Context {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return ctx
	}
	m := MetaFrom(ctx)
	m.WorkflowID = workflowID
	return WithMeta(ctx, m)
}

func WithPlan(ctx context.Context, planID string) context.Context {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return ctx
	}
	m := MetaFrom(ctx)
	m.PlanID = planID
	return WithMeta(ctx, m)
}

func BindIdentity(ctx context.Context, id *auth.Identity, mcpClient string) context.Context {
	m := MetaFrom(ctx)
	m.MCPClient = sanitizeMCPClient(mcpClient)
	if t := oplogs.TraceFrom(ctx); t != "" {
		m.TraceID = t
	}
	if id == nil {
		if m.ActorType == "" {
			m.ActorType = store.ActorTypeSystem
		}
		return WithMeta(ctx, m)
	}
	switch id.Kind {
	case auth.KindAI:
		m.ActorType = store.ActorTypeAISession
		m.ActorID = id.CredID
		m.AISessionID = id.CredID
		m.ParentActor = id.ParentEmail
		if id.CredName != "" {
			m.ActorName = id.CredName
		} else {
			m.ActorName = sessionNameFromActor(id.Actor)
		}
	case auth.KindUser:
		m.ActorType = store.ActorTypeUser
		m.ActorID = id.UserID
		m.ActorName = id.Email
	case auth.KindAPI:
		m.ActorType = store.ActorTypeUser
		m.ActorID = id.CredID
		if id.CredName != "" {
			m.ActorName = id.CredName
		} else {
			m.ActorName = id.Actor
		}
	default:
		m.ActorType = store.ActorTypeSystem
		m.ActorID = id.DeviceID
		m.ActorName = id.Actor
	}
	return WithMeta(ctx, m)
}

func sessionNameFromActor(actor string) string {
	const p = "ai-session:"
	if !strings.HasPrefix(actor, p) {
		return actor
	}
	rest := strings.TrimPrefix(actor, p)
	if i := strings.Index(rest, " parent:"); i >= 0 {
		return strings.TrimSpace(rest[:i])
	}
	return strings.TrimSpace(rest)
}

func sanitizeMCPClient(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

func inferActor(actor, userID string) (actorType, actorID, parent, sessionID, name string) {
	if strings.HasPrefix(actor, "ai-session:") {
		name = sessionNameFromActor(actor)
		parent = ""
		if i := strings.Index(actor, " parent:"); i >= 0 {
			parent = strings.TrimSpace(actor[i+len(" parent:"):])
		}
		return store.ActorTypeAISession, "", parent, "", name
	}
	switch actor {
	case "agent", "refresh":
		return store.ActorTypeSystem, "", "", "", actor
	}
	if userID != "" {
		return store.ActorTypeUser, userID, "", "", actor
	}
	return store.ActorTypeSystem, "", "", "", actor
}
