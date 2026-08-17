package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		apierrors.Write(w, apierrors.Internal("secrets unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Secrets.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list secrets"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "secrets.list", "", "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]any{"secrets": list})
}

func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		apierrors.Write(w, apierrors.Internal("secrets unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rec, err := s.Secrets.Create(r.Context(), id.UserID, body.Name, body.Value)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "secrets.create", body.Name, "", "FAILURE")
		writeSecretErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "secrets.create", rec.ID, rec.Name, "SUCCESS")
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleGetSecret(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		apierrors.Write(w, apierrors.Internal("secrets unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	rec, err := s.Secrets.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeSecretErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleRotateSecret(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		apierrors.Write(w, apierrors.Internal("secrets unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rec, err := s.Secrets.Rotate(r.Context(), id.UserID, r.PathValue("id"), body.Value)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "secrets.rotate", r.PathValue("id"), "", "FAILURE")
		writeSecretErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "secrets.rotate", rec.ID, rec.Name+" v"+strconv.Itoa(rec.Version), "SUCCESS")
	writeJSON(w, http.StatusOK, rec)
}

func writeSecretErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, secrets.ErrConflict):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, secrets.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
