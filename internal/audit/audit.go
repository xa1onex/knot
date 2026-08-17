package audit

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
)

var secretRe = regexp.MustCompile(`(?i)(secret|password|token|api[_-]?key|credential|private)[=:][^\s,;]+`)

func Sanitize(s string) string {
	if s == "" {
		return s
	}
	return secretRe.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.IndexAny(m, "=:")
		if i < 0 {
			return "[redacted]"
		}
		return m[:i+1] + "[redacted]"
	})
}

type Logger struct {
	Store *store.Store
	Logs  *oplogs.Service
}

func (l *Logger) Log(ctx context.Context, userID, actor, action, resource, detail, result string) {
	detail = Sanitize(detail)
	meta := MetaFrom(ctx)
	e := &store.AuditEvent{
		UserID:      userID,
		Actor:       actor,
		Action:      action,
		Resource:    resource,
		Detail:      detail,
		Result:      result,
		ActorType:   meta.ActorType,
		ActorID:     meta.ActorID,
		ParentActor: meta.ParentActor,
		AISessionID: meta.AISessionID,
		MCPClient:   meta.MCPClient,
		WorkflowID:  meta.WorkflowID,
		PlanID:      meta.PlanID,
		TraceID:     firstNonEmpty(meta.TraceID, oplogs.TraceFrom(ctx)),
	}
	if e.ActorType == "" {
		var name string
		e.ActorType, e.ActorID, e.ParentActor, e.AISessionID, name = inferActor(actor, userID)
		if name != "" {
			e.Actor = name
		}
	}
	if meta.ActorName != "" {
		e.Actor = meta.ActorName
	}
	if err := l.Store.InsertAudit(ctx, e); err != nil {
		log.Printf("audit_write_failed action=%s actor=%s err=%v", action, actor, err)
	}
	if l == nil || l.Logs == nil || userID == "" {
		return
	}
	level := "info"
	if result == "FAILURE" || result == "DENIED" {
		level = "error"
	}
	msg := action
	if resource != "" {
		msg += " " + resource
	}
	if detail != "" {
		msg += " " + detail
	}
	if result != "" {
		msg += " " + result
	}
	l.Logs.Emit(ctx, oplogs.Event{
		UserID: userID, Source: oplogs.SourceAudit, Level: level, Message: msg,
		TraceID: e.TraceID,
		Metadata: map[string]any{
			"actor": e.Actor, "action": action, "result": result,
			"actor_type": e.ActorType, "parent": e.ParentActor,
			"ai_session_id": e.AISessionID, "mcp_client": e.MCPClient,
			"workflow_id": e.WorkflowID, "plan_id": e.PlanID,
		},
	})
}
