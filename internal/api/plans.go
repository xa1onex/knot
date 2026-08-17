package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/plans"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Plans.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list plans"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, planJSON(&list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalog": plans.Catalog(), "plans": out})
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Intent       string `json:"intent"`
		Name         string `json:"name"`
		Plan         string `json:"plan"`
		Service      string `json:"service"`
		DeviceID     string `json:"device_id"`
		Image        string `json:"image"`
		BuildID      string `json:"build_id"`
		Port         int    `json:"port"`
		Hostname     string `json:"hostname"`
		Environment  string `json:"environment"`
		Query        string `json:"query"`
		Path         string `json:"path"`
		FromDeviceID string `json:"from_device_id"`
		ToDeviceID   string `json:"to_device_id"`
		ToPath       string `json:"to_path"`
		JobImage     string `json:"job_image"`
		TTLMinutes   int    `json:"ttl_minutes"`
		ExpiresIn    string `json:"expires_in"`
		AutoExecute  bool   `json:"auto_execute"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = strings.TrimSpace(body.Plan)
	}
	p, err := s.Plans.Create(r.Context(), plans.CreateRequest{
		UserID: id.UserID, Actor: id.Actor, Kind: id.Kind, CredID: id.CredID, Email: id.Email,
		Intent: body.Intent, Name: name, Service: body.Service, DeviceID: body.DeviceID,
		Image: body.Image, BuildID: body.BuildID, Port: body.Port, Hostname: body.Hostname,
		Environment: body.Environment, Query: body.Query, Path: body.Path,
		FromDeviceID: body.FromDeviceID, ToDeviceID: body.ToDeviceID, ToPath: body.ToPath,
		JobImage: body.JobImage, TTLMinutes: body.TTLMinutes, ExpiresIn: body.ExpiresIn,
		AutoExecute: body.AutoExecute,
	})
	if err != nil {
		writePlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, planJSON(p, true))
}

func (s *Server) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	p, err := s.Plans.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writePlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, planJSON(p, true))
}

func (s *Server) handleApprovePlan(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	if id.Kind == auth.KindAI {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "plan.approve", r.PathValue("id"), "ai_session", "DENIED")
		apierrors.Write(w, apierrors.Forbidden("AI session cannot approve a plan"))
		return
	}
	p, err := s.Plans.Approve(r.Context(), planActor(id), r.PathValue("id"))
	if err != nil {
		writePlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, planJSON(p, true))
}

func (s *Server) handleExecutePlan(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	p, err := s.Plans.Execute(r.Context(), planActor(id), r.PathValue("id"))
	if err != nil {
		writePlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, planJSON(p, true))
}

func (s *Server) handleCancelPlan(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		apierrors.Write(w, apierrors.Internal("plans unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	p, err := s.Plans.Cancel(r.Context(), id.UserID, r.PathValue("id"), id.Actor)
	if err != nil {
		writePlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, planJSON(p, true))
}

func planActor(id *auth.Identity) plans.ActorRequest {
	return plans.ActorRequest{
		UserID: id.UserID, Actor: id.Actor, Kind: id.Kind, CredID: id.CredID, Email: id.Email, Can: id.Has,
	}
}

func writePlanErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, plans.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, plans.ErrValidation), errors.Is(err, plans.ErrState), errors.Is(err, plans.ErrExpired):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, plans.ErrForbidden), errors.Is(err, plans.ErrDenied):
		apierrors.Write(w, apierrors.Forbidden(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}

func planJSON(p *store.Plan, withSteps bool) map[string]any {
	out := map[string]any{
		"id": p.ID, "name": p.Name, "title": p.Title, "intent": p.Intent,
		"created_by": p.CreatedBy, "ai_session_id": p.AISessionID, "actor": p.Actor,
		"trace_id": p.TraceID, "risk_level": p.RiskLevel, "status": p.Status,
		"requires_approval": plans.NeedsApproval(p.RiskLevel),
		"error":             p.Error, "result": jsonObject(p.ResultJSON), "input": jsonObject(p.InputJSON),
		"approved_by": p.ApprovedBy,
		"created_at":  p.CreatedAt, "updated_at": p.UpdatedAt, "expires_at": p.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	if p.ApprovedAt != nil {
		out["approved_at"] = p.ApprovedAt.UTC().Format(time.RFC3339Nano)
	}
	if p.FinishedAt != nil {
		out["finished_at"] = p.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if withSteps {
		steps := make([]map[string]any, 0, len(p.Steps))
		for i := range p.Steps {
			st := &p.Steps[i]
			step := map[string]any{
				"id": st.ID, "seq": st.Seq, "name": st.Name, "title": st.Title,
				"status": st.Status, "scope": st.Scope, "risk_level": st.RiskLevel,
				"error": st.Error, "output": jsonObject(st.OutputJSON), "trace_id": st.TraceID,
			}
			if st.StartedAt != nil {
				step["started_at"] = st.StartedAt.UTC().Format(time.RFC3339Nano)
			}
			if st.FinishedAt != nil {
				step["finished_at"] = st.FinishedAt.UTC().Format(time.RFC3339Nano)
			}
			steps = append(steps, step)
		}
		out["steps"] = steps
	}
	return out
}
