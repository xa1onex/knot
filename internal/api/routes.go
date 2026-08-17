package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	if s.Edge == nil {
		apierrors.Write(w, apierrors.Internal("edge unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Edge.ListRoutes(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list routes"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, routeJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	if s.Edge == nil {
		apierrors.Write(w, apierrors.Internal("edge unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Hostname     string `json:"hostname"`
		ServiceID    string `json:"service_id"`
		EdgeDeviceID string `json:"edge_device_id"`
		TLSMode      string `json:"tls_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	rt, err := s.Edge.CreateRoute(r.Context(), edge.CreateRouteRequest{
		UserID:       id.UserID,
		Hostname:     body.Hostname,
		ServiceID:    body.ServiceID,
		EdgeDeviceID: body.EdgeDeviceID,
		TLSMode:      body.TLSMode,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "routes.create", body.Hostname, err.Error(), "FAILURE")
		writeServiceErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "routes.create", rt.ID, rt.Hostname, "SUCCESS")
	writeJSON(w, http.StatusCreated, routeJSON(rt))
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	if s.Edge == nil {
		apierrors.Write(w, apierrors.Internal("edge unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	rid := r.PathValue("id")
	if err := s.Edge.DeleteRoute(r.Context(), id.UserID, rid); err != nil {
		writeServiceErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "routes.delete", rid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleServiceHealth(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	svc, err := s.Services.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.serviceHealthJSON(r, svc, true))
}

func (s *Server) serviceHealthJSON(r *http.Request, svc *store.Service, probe bool) map[string]any {
	out := serviceJSON(svc)
	if s.Edge == nil {
		return out
	}
	h := s.Edge.Health(r.Context(), svc, probe)
	out["registered"] = h.Registered
	out["agent_online"] = h.AgentOnline
	out["tunnel_connected"] = h.TunnelConnected
	out["backend_reachable"] = h.BackendReachable
	out["edge_device_id"] = h.EdgeDeviceID
	out["edge_device_name"] = h.EdgeDeviceName
	out["edge_online"] = h.EdgeOnline
	out["hostnames"] = h.Hostnames
	if h.Error != "" {
		out["health_error"] = h.Error
	}
	return out
}

func fmtListen(rt *store.EdgeRoute) string {
	return fmt.Sprintf("%s://%s:%d", rt.Protocol, rt.Bind, rt.Port)
}
