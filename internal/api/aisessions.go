package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/knot-infra/knot/internal/aisessions"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListAISessions(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		apierrors.Write(w, apierrors.Internal("AI sessions unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.AI.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list AI sessions"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *Server) handleCreateAISession(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		apierrors.Write(w, apierrors.Internal("AI sessions unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	if id == nil || id.Kind == auth.KindAI {
		apierrors.Write(w, apierrors.Forbidden("AI session cannot create another AI session"))
		return
	}
	var body struct {
		Name       string   `json:"name"`
		Scopes     []string `json:"scopes"`
		TTLMinutes int      `json:"ttl_minutes"`
		ExpiresIn  string   `json:"expires_in"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	sess, err := s.AI.Create(r.Context(), aisessions.CreateRequest{
		UserID: id.UserID, Actor: id.Actor, CreatorKind: id.Kind, CreatorScope: id.Scopes,
		Name: body.Name, Scopes: body.Scopes, TTLMinutes: body.TTLMinutes, ExpiresIn: body.ExpiresIn,
	})
	if err != nil {
		writeAISessionErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "ai.session.create", sess.ID, strings.Join(sess.Scopes, ","), "SUCCESS")
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleGetAISession(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		apierrors.Write(w, apierrors.Internal("AI sessions unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	sess, err := s.AI.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeAISessionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleCurrentAISession(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		apierrors.Write(w, apierrors.Internal("AI sessions unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	sess, err := s.AI.Current(r.Context(), id)
	if err != nil {
		writeAISessionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleRevokeAISession(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		apierrors.Write(w, apierrors.Internal("AI sessions unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	cid := r.PathValue("id")
	if err := s.AI.Revoke(r.Context(), id.UserID, cid); err != nil {
		writeAISessionErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "ai.session.revoke", cid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func writeAISessionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aisessions.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, aisessions.ErrForbidden):
		apierrors.Write(w, apierrors.Forbidden(err.Error()))
	case errors.Is(err, aisessions.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
