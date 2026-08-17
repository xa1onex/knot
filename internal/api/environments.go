package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	if s.Environments == nil {
		apierrors.Write(w, apierrors.Internal("environments unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Environments.List(r.Context(), id.UserID, r.URL.Query().Get("project"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list environments"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": list})
}

func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	if s.Environments == nil {
		apierrors.Write(w, apierrors.Internal("environments unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Project string            `json:"project"`
		Name    string            `json:"name"`
		Vars    map[string]string `json:"vars"`
		Secrets map[string]string `json:"secrets"`
		Policy  map[string]string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rec, err := s.Environments.Create(r.Context(), environments.CreateRequest{
		UserID: id.UserID, Project: body.Project, Name: body.Name,
		Vars: body.Vars, Secrets: body.Secrets, Policy: body.Policy,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "env.create", body.Name, "", "FAILURE")
		writeEnvErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "env.create", rec.ID, rec.Name, "SUCCESS")
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	if s.Environments == nil {
		apierrors.Write(w, apierrors.Internal("environments unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	rec, err := s.Environments.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	if s.Environments == nil {
		apierrors.Write(w, apierrors.Internal("environments unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Vars    map[string]string `json:"vars"`
		Secrets map[string]string `json:"secrets"`
		Policy  map[string]string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rec, err := s.Environments.Update(r.Context(), id.UserID, r.PathValue("id"), body.Vars, body.Secrets, body.Policy)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "env.update", r.PathValue("id"), "", "FAILURE")
		writeEnvErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "env.update", rec.ID, rec.Name, "SUCCESS")
	writeJSON(w, http.StatusOK, rec)
}

func writeEnvErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, environments.ErrNotFound), errors.Is(err, secrets.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, environments.ErrConflict):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, environments.ErrValidation), errors.Is(err, secrets.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
