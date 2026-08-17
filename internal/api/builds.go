package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/knot-infra/knot/internal/builds"
	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Builds.ListSources(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list sources"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, sourceJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Type               string `json:"type"`
		Name               string `json:"name"`
		URL                string `json:"url"`
		Branch             string `json:"branch"`
		GitTag             string `json:"git_tag"`
		Revision           string `json:"revision"`
		CredentialSecretID string `json:"credential_secret_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	src, err := s.Builds.CreateSource(r.Context(), builds.CreateSourceRequest{
		UserID: id.UserID, Type: body.Type, Name: body.Name, URL: body.URL,
		Branch: body.Branch, GitTag: body.GitTag, Revision: body.Revision,
		CredentialSecretID: body.CredentialSecretID,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "sources.create", body.URL, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeBuildErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "sources.create", src.ID, src.URL, "SUCCESS")
	writeJSON(w, http.StatusCreated, sourceJSON(src))
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	src, err := s.Builds.GetSource(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeBuildErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceJSON(src))
}

func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Builds.List(r.Context(), id.UserID, r.URL.Query().Get("source_id"), r.URL.Query().Get("device_id"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list builds"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, buildJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": out})
}

func (s *Server) handleCreateBuild(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		SourceID         string `json:"source_id"`
		DeviceID         string `json:"device_id"`
		Dockerfile       string `json:"dockerfile"`
		Context          string `json:"context"`
		Tag              string `json:"tag"`
		TimeoutSeconds   int    `json:"timeout_seconds"`
		RegistrySecretID string `json:"registry_secret_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	b, err := s.Builds.Create(r.Context(), builds.CreateBuildRequest{
		UserID: id.UserID, SourceID: body.SourceID, DeviceID: body.DeviceID,
		Dockerfile: body.Dockerfile, Context: body.Context, Tag: body.Tag,
		TimeoutSeconds: body.TimeoutSeconds, RegistrySecretID: body.RegistrySecretID,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "builds.create", body.SourceID, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeBuildErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "builds.create", b.ID, b.Tag, "SUCCESS")
	writeJSON(w, http.StatusCreated, buildJSON(b))
}

func (s *Server) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	b, err := s.Builds.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeBuildErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, buildJSON(b))
}

func (s *Server) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	if s.Builds == nil {
		apierrors.Write(w, apierrors.Internal("builds unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.Builds.Logs(r.Context(), id.UserID, r.PathValue("id"), limit)
	if err != nil {
		writeBuildErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		out = append(out, map[string]any{
			"id": l.ID, "stream": l.Stream, "message": l.Message, "created_at": l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

func sourceJSON(src *store.AppSource) map[string]any {
	out := map[string]any{
		"id": src.ID, "type": src.Type, "name": src.Name, "url": src.URL,
		"branch": src.Branch, "git_tag": src.GitTag, "revision": src.Revision,
		"created_at": src.CreatedAt, "updated_at": src.UpdatedAt,
	}
	if src.CredentialSecretID != "" {
		out["credential_secret_id"] = src.CredentialSecretID
	}
	return out
}

func buildJSON(b *store.Build) map[string]any {
	return map[string]any{
		"id": b.ID, "source_id": b.SourceID, "device_id": b.DeviceID,
		"device_name": b.DeviceName, "device_online": b.DeviceOnline,
		"dockerfile": b.Dockerfile, "context": b.Context, "tag": b.Tag, "image": b.Image,
		"status": b.Status, "error": b.Error, "revision": b.GitRevision,
		"timeout_seconds": b.TimeoutSeconds, "created_at": b.CreatedAt,
		"started_at": b.StartedAt, "finished_at": b.FinishedAt,
		"trace_id": b.TraceID,
	}
}

func writeBuildErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, builds.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, builds.ErrDeviceOffline), errors.Is(err, builds.ErrConflict):
		apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
	case errors.Is(err, builds.ErrDevice), errors.Is(err, builds.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, secrets.ErrNotFound):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
