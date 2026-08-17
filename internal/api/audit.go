package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleAuditSearch(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	q := parseAuditQuery(r, id.UserID)
	list, err := s.Store.SearchAudit(r.Context(), q)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to search audit"))
		return
	}
	events := make([]map[string]any, 0, len(list))
	for _, e := range list {
		events = append(events, audit.EventView(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleAuditAI(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	q := parseAuditQuery(r, id.UserID)
	q.ActorType = store.ActorTypeAISession
	list, err := s.Store.SearchAudit(r.Context(), q)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list AI activity"))
		return
	}
	wfs := map[string]*store.Workflow{}
	if s.Workflows != nil {
		seen := map[string]bool{}
		for _, e := range list {
			if e.WorkflowID == "" || seen[e.WorkflowID] {
				continue
			}
			seen[e.WorkflowID] = true
			if wf, err := s.Workflows.Get(r.Context(), id.UserID, e.WorkflowID); err == nil {
				wfs[e.WorkflowID] = wf
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": audit.BuildAIActivity(list, wfs)})
}

func (s *Server) handleAuditTrace(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	traceID := strings.TrimSpace(r.PathValue("id"))
	if traceID == "" {
		apierrors.Write(w, apierrors.Validation("trace_id required"))
		return
	}
	list, err := s.Store.SearchAudit(r.Context(), store.AuditQuery{
		UserID: id.UserID, TraceID: traceID, Limit: 200,
	})
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to load trace"))
		return
	}
	events := make([]map[string]any, 0, len(list))
	for _, e := range list {
		events = append(events, audit.EventView(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trace_id": traceID, "events": events})
}

func parseAuditQuery(r *http.Request, userID string) store.AuditQuery {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return store.AuditQuery{
		UserID:      userID,
		ActorType:   r.URL.Query().Get("actor_type"),
		ActorID:     r.URL.Query().Get("actor_id"),
		AISessionID: firstQuery(r, "ai_session_id", "session_id"),
		WorkflowID:  r.URL.Query().Get("workflow_id"),
		TraceID:     r.URL.Query().Get("trace_id"),
		Action:      r.URL.Query().Get("action"),
		MCPClient:   r.URL.Query().Get("mcp_client"),
		Q:           firstQuery(r, "q", "query"),
		Limit:       limit,
	}
}

func firstQuery(r *http.Request, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			return v
		}
	}
	return ""
}
