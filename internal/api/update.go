package api

import (
	"errors"
	"net/http"

	"github.com/knot-infra/knot/internal/selfupdate"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	if s.Updates == nil || id == nil {
		apierrors.Write(w, apierrors.Internal("updates unavailable"))
		return
	}
	out, err := s.Updates.Fleet(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateControlPlane(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		apierrors.Write(w, apierrors.Internal("updates unavailable"))
		return
	}
	status, logs, err := s.Updates.ApplyControlPlane(r.Context(), r.URL.Query().Get("force") == "1")
	if err != nil && !errors.Is(err, selfupdate.ErrUnavailable) {
		apierrors.Write(w, apierrors.Internal(err.Error()))
		return
	}
	if errors.Is(err, selfupdate.ErrUnavailable) {
		apierrors.Write(w, apierrors.Validation("control-plane self-update unavailable on this install"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status, "logs": logs})
}

func (s *Server) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		apierrors.Write(w, apierrors.Internal("updates unavailable"))
		return
	}
	status, logs, err := s.Updates.ApplyDevice(r.Context(), r.PathValue("id"), r.URL.Query().Get("force") == "1")
	if err != nil {
		apierrors.Write(w, apierrors.Internal(err.Error()))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status, "logs": logs})
}
