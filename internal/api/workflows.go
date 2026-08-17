package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/workflows"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if s.Workflows == nil {
		apierrors.Write(w, apierrors.Internal("workflows unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Workflows.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list workflows"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, workflowJSON(&list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalog": workflows.Catalog(), "workflows": out})
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.Workflows == nil {
		apierrors.Write(w, apierrors.Internal("workflows unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	if id == nil {
		apierrors.Write(w, apierrors.Unauthorized("unauthorized"))
		return
	}
	var body struct {
		Name         string `json:"name"`
		Workflow     string `json:"workflow"`
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = strings.TrimSpace(body.Workflow)
	}
	wf, err := s.Workflows.Run(r.Context(), workflows.RunRequest{
		UserID: id.UserID, Actor: id.Actor, CredID: id.CredID, Name: name,
		Service: body.Service, DeviceID: body.DeviceID, Image: body.Image, BuildID: body.BuildID,
		Port: body.Port, Hostname: body.Hostname, Environment: body.Environment,
		Query: body.Query, Path: body.Path, FromDeviceID: body.FromDeviceID,
		ToDeviceID: body.ToDeviceID, ToPath: body.ToPath, JobImage: body.JobImage,
		Can: id.Has,
	})
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workflowJSON(wf, true))
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.Workflows == nil {
		apierrors.Write(w, apierrors.Internal("workflows unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	wf, err := s.Workflows.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowJSON(wf, true))
}

func (s *Server) handleWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	if s.Workflows == nil {
		apierrors.Write(w, apierrors.Internal("workflows unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	steps, err := s.Workflows.Steps(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(steps))
	for i := range steps {
		out = append(out, workflowStepJSON(&steps[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": out})
}

func writeWorkflowErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflows.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, workflows.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}

func workflowJSON(wf *store.Workflow, withSteps bool) map[string]any {
	out := map[string]any{
		"id": wf.ID, "name": wf.Name, "title": wf.Title, "actor": wf.Actor,
		"status": wf.Status, "trace_id": wf.TraceID, "error": wf.Error,
		"result": jsonObject(wf.ResultJSON), "created_at": wf.CreatedAt,
		"updated_at": wf.UpdatedAt,
	}
	if wf.FinishedAt != nil {
		out["finished_at"] = wf.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if withSteps {
		steps := make([]map[string]any, 0, len(wf.Steps))
		for i := range wf.Steps {
			steps = append(steps, workflowStepJSON(&wf.Steps[i]))
		}
		out["steps"] = steps
	}
	return out
}

func workflowStepJSON(st *store.WorkflowStep) map[string]any {
	out := map[string]any{
		"id": st.ID, "seq": st.Seq, "name": st.Name, "status": st.Status,
		"scope": st.Scope, "error": st.Error, "output": jsonObject(st.OutputJSON),
		"trace_id": st.TraceID,
	}
	if st.StartedAt != nil {
		out["started_at"] = st.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if st.FinishedAt != nil {
		out["finished_at"] = st.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func jsonObject(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}
