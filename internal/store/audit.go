package store

import (
	"context"
	"strings"
	"time"
)

type AuditQuery struct {
	UserID      string
	ActorType   string
	ActorID     string
	AISessionID string
	WorkflowID  string
	PlanID      string
	TraceID     string
	Action      string
	MCPClient   string
	Q           string
	Limit       int
}

func (s *Store) SearchAudit(ctx context.Context, q AuditQuery) ([]AuditEvent, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	args := []any{q.UserID}
	var b strings.Builder
	b.WriteString(`SELECT id, COALESCE(user_id, ''), actor, action, resource, detail, result, created_at,
  COALESCE(actor_type, 'user'), COALESCE(actor_id, ''), COALESCE(parent_actor, ''),
  COALESCE(ai_session_id, ''), COALESCE(mcp_client, ''), COALESCE(workflow_id, ''), COALESCE(plan_id, ''), COALESCE(trace_id, '')
FROM audit_events WHERE user_id = ?`)
	if v := strings.TrimSpace(q.ActorType); v != "" {
		b.WriteString(` AND actor_type = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.ActorID); v != "" {
		b.WriteString(` AND actor_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.AISessionID); v != "" {
		b.WriteString(` AND ai_session_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.WorkflowID); v != "" {
		b.WriteString(` AND workflow_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.PlanID); v != "" {
		b.WriteString(` AND plan_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.TraceID); v != "" {
		b.WriteString(` AND trace_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Action); v != "" {
		b.WriteString(` AND action = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.MCPClient); v != "" {
		b.WriteString(` AND mcp_client = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Q); v != "" {
		like := "%" + v + "%"
		b.WriteString(` AND (action LIKE ? OR actor LIKE ? OR resource LIKE ? OR detail LIKE ? OR parent_actor LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	b.WriteString(` ORDER BY created_at DESC LIMIT ?`)
	args = append(args, q.Limit)

	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var created string
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.Actor, &e.Action, &e.Resource, &e.Detail, &e.Result, &created,
			&e.ActorType, &e.ActorID, &e.ParentActor, &e.AISessionID, &e.MCPClient, &e.WorkflowID, &e.PlanID, &e.TraceID,
		); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.fillTarget()
		out = append(out, e)
	}
	if out == nil {
		out = []AuditEvent{}
	}
	return out, rows.Err()
}
