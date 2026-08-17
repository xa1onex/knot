package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleRouteTraffic(w http.ResponseWriter, r *http.Request) {
	if s.Traffic == nil {
		apierrors.Write(w, apierrors.Internal("traffic unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	st, err := s.Traffic.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeTrafficErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, trafficJSON(st))
}

func (s *Server) handleRouteTrafficSwitch(w http.ResponseWriter, r *http.Request) {
	if s.Traffic == nil {
		apierrors.Write(w, apierrors.Internal("traffic unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		ReleaseID string `json:"release_id"`
		Weight    int    `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	st, err := s.Traffic.Switch(r.Context(), traffic.SwitchRequest{
		UserID: id.UserID, Actor: id.Actor, Route: r.PathValue("id"),
		ReleaseID: body.ReleaseID, Weight: body.Weight,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "traffic.switch", r.PathValue("id"), err.Error(), "FAILURE")
		writeTrafficErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "traffic.switch", st.RouteID, st.Hostname+" → "+st.ActiveReleaseID, "SUCCESS")
	writeJSON(w, http.StatusOK, trafficJSON(st))
}

func (s *Server) handleRouteTrafficRollback(w http.ResponseWriter, r *http.Request) {
	if s.Traffic == nil {
		apierrors.Write(w, apierrors.Internal("traffic unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	st, err := s.Traffic.Rollback(r.Context(), id.UserID, id.Actor, r.PathValue("id"))
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "traffic.rollback", r.PathValue("id"), err.Error(), "FAILURE")
		writeTrafficErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "traffic.rollback", st.RouteID, st.Hostname+" → "+st.ActiveReleaseID, "SUCCESS")
	writeJSON(w, http.StatusOK, trafficJSON(st))
}

func trafficJSON(st *traffic.Status) map[string]any {
	targets := make([]map[string]any, 0, len(st.Targets))
	for _, t := range st.Targets {
		targets = append(targets, map[string]any{
			"release_id": t.ReleaseID, "number": t.Number, "image": t.Image,
			"status": t.Status, "weight": t.Weight, "current": t.Current,
			"port": t.Port, "device_id": t.DeviceID,
		})
	}
	hist := make([]map[string]any, 0, len(st.History))
	for _, h := range st.History {
		hist = append(hist, map[string]any{
			"id": h.ID, "action": h.Action, "from_release_id": h.FromReleaseID,
			"to_release_id": h.ToReleaseID, "weights": json.RawMessage(h.WeightsJSON),
			"created_by": h.CreatedBy, "created_at": h.CreatedAt,
		})
	}
	return map[string]any{
		"route_id":          st.RouteID,
		"hostname":          st.Hostname,
		"service_id":        st.ServiceID,
		"service":           st.ServiceName,
		"tls_mode":          st.TLSMode,
		"active_release_id": st.ActiveReleaseID,
		"prev_release_id":   st.PrevReleaseID,
		"targets":           targets,
		"history":           hist,
	}
}

func writeTrafficErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, traffic.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, traffic.ErrNothingToDo), errors.Is(err, traffic.ErrConflict):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, traffic.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}

func routeJSON(rt *store.EdgeRoute) map[string]any {
	return map[string]any{
		"id":                rt.ID,
		"hostname":          rt.Hostname,
		"service_id":        rt.ServiceID,
		"service_name":      rt.ServiceName,
		"device_id":         rt.DeviceID,
		"device_name":       rt.DeviceName,
		"edge_device_id":    rt.EdgeDeviceID,
		"edge_device_name":  rt.EdgeName,
		"tls_mode":          rt.TLSMode,
		"listen":            fmtListen(rt),
		"active_release_id": rt.ActiveReleaseID,
		"prev_release_id":   rt.PrevReleaseID,
		"created_at":        rt.CreatedAt,
	}
}
