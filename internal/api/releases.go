package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Releases.List(r.Context(), id.UserID, r.URL.Query().Get("service"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list releases"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, releaseJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

func (s *Server) handleCreateRelease(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Service              string `json:"service"`
		Name                 string `json:"name"`
		Image                string `json:"image"`
		Environment          string `json:"environment"`
		EnvironmentID        string `json:"environment_id"`
		Project              string `json:"project"`
		DeviceID             string `json:"device_id"`
		Port                 int    `json:"port"`
		Bind                 string `json:"bind"`
		HealthPath           string `json:"health_path"`
		HealthTimeoutSeconds int    `json:"health_timeout_seconds"`
		HealthRetries        int    `json:"health_retries"`
		HealthExpectedStatus int    `json:"health_expected_status"`
		Hostname             string `json:"hostname"`
		EdgeDeviceID         string `json:"edge_device_id"`
		BuildID              string `json:"build_id"`
		JobID                string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	svc := body.Service
	if svc == "" {
		svc = body.Name
	}
	rel, err := s.Releases.Create(r.Context(), releases.CreateRequest{
		UserID: id.UserID, CreatedBy: id.Actor, Service: svc, Image: body.Image,
		Environment: body.Environment, EnvironmentID: body.EnvironmentID, Project: body.Project,
		DeviceID: body.DeviceID, Port: body.Port, Bind: body.Bind, HealthPath: body.HealthPath,
		HealthTimeoutSeconds: body.HealthTimeoutSeconds, HealthRetries: body.HealthRetries,
		HealthExpectedStatus: body.HealthExpectedStatus, Hostname: body.Hostname,
		EdgeDeviceID: body.EdgeDeviceID, BuildID: body.BuildID, JobID: body.JobID,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.create", svc, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeReleaseErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.create", rel.ID, fmtReleaseAudit(rel), "SUCCESS")
	writeJSON(w, http.StatusCreated, releaseJSON(rel))
}

func (s *Server) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	rel, err := s.Releases.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeReleaseErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseJSON(rel))
}

func (s *Server) handleDeployRelease(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
		Port     int    `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rel, err := s.Releases.Deploy(r.Context(), releases.DeployRequest{
		UserID: id.UserID, ID: r.PathValue("id"), DeviceID: body.DeviceID, Port: body.Port,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.deploy", r.PathValue("id"), deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeReleaseErr(w, err)
		return
	}
	if rel == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	outcome := "SUCCESS"
	if rel.Status == store.ReleaseStatusFailed {
		outcome = "FAILURE"
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.deploy", rel.ID, fmtReleaseAudit(rel), outcome)
	writeJSON(w, http.StatusOK, releaseJSON(rel))
}

func (s *Server) handleRollbackRelease(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	relID := r.PathValue("id")
	rel, err := s.Releases.Rollback(r.Context(), id.UserID, relID)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.rollback", relID, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeReleaseErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "releases.rollback", rel.ID, fmtReleaseAudit(rel), "SUCCESS")
	writeJSON(w, http.StatusOK, releaseJSON(rel))
}

func (s *Server) handleReleaseLogs(w http.ResponseWriter, r *http.Request) {
	if s.Releases == nil {
		apierrors.Write(w, apierrors.Internal("releases unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.Releases.Logs(r.Context(), id.UserID, r.PathValue("id"), limit)
	if err != nil {
		writeReleaseErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		out = append(out, map[string]any{
			"id": l.ID, "stream": l.Stream, "source": l.Source, "message": l.Message, "created_at": l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

func releaseJSON(rel *store.Release) map[string]any {
	return map[string]any{
		"id":                     rel.ID,
		"number":                 rel.Number,
		"service":                rel.Service,
		"image":                  rel.Image,
		"environment_id":         rel.EnvironmentID,
		"environment":            rel.EnvironmentName,
		"config_version":         rel.ConfigVersion,
		"secrets":                deploy.ParseSecretPins(rel.SecretPinsJSON),
		"status":                 rel.Status,
		"created_by":             rel.CreatedBy,
		"device_id":              rel.DeviceID,
		"device_name":            rel.DeviceName,
		"port":                   rel.Port,
		"bind":                   rel.Bind,
		"health_path":            rel.HealthPath,
		"health_timeout_seconds": rel.HealthTimeoutSeconds,
		"health_retries":         rel.HealthRetries,
		"health_expected_status": rel.HealthExpectedStatus,
		"build_id":               rel.BuildID,
		"job_id":                 rel.JobID,
		"deployment_id":          rel.DeploymentID,
		"prev_release_id":        rel.PrevReleaseID,
		"current":                rel.Current,
		"error":                  rel.Error,
		"trace_id":               rel.TraceID,
		"created_at":             rel.CreatedAt,
		"updated_at":             rel.UpdatedAt,
	}
}

func fmtReleaseAudit(rel *store.Release) string {
	return rel.Service + " #" + strconv.Itoa(rel.Number) + " " + rel.Image
}

func writeReleaseErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, releases.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, releases.ErrDeviceOffline):
		apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
	case errors.Is(err, releases.ErrUnhealthy):
		apierrors.WriteCode(w, http.StatusConflict, "unhealthy", err.Error())
	case errors.Is(err, releases.ErrNothingToRoll), errors.Is(err, releases.ErrConflict):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, releases.ErrValidation), errors.Is(err, environments.ErrValidation), errors.Is(err, secrets.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, environments.ErrNotFound), errors.Is(err, secrets.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
