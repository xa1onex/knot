package api

import (
	"encoding/json"
	"net/http"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListCompute(w http.ResponseWriter, r *http.Request) {
	if s.Compute == nil {
		apierrors.Write(w, apierrors.Internal("compute unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Compute.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list compute"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "compute.list", "", "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]any{"devices": list})
}

func (s *Server) handleGetCompute(w http.ResponseWriter, r *http.Request) {
	if s.Compute == nil {
		apierrors.Write(w, apierrors.Internal("compute unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	devID := r.PathValue("device_id")
	rec, err := s.Compute.Get(r.Context(), id.UserID, devID)
	if err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("device not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("failed to get compute"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "compute.get", devID, "", "SUCCESS")
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handlePutComputeLabels(w http.ResponseWriter, r *http.Request) {
	if s.Compute == nil {
		apierrors.Write(w, apierrors.Internal("compute unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	devID := r.PathValue("device_id")
	var body struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rec, err := s.Compute.SetLabels(r.Context(), id.UserID, devID, body.Labels)
	if err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("device not found"))
			return
		}
		apierrors.Write(w, apierrors.Validation(err.Error()))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "compute.labels", devID, "", "SUCCESS")
	writeJSON(w, http.StatusOK, rec)
}
