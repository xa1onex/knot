package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Services.List(r.Context(), id.UserID, r.URL.Query().Get("device_id"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list services"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, s.serviceHealthJSON(r, &list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}

func (s *Server) handleServicesTree(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	tree, err := s.Services.Tree(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list services"))
		return
	}
	nodes := make([]map[string]any, 0, len(tree))
	for i := range tree {
		n := &tree[i]
		svcs := make([]map[string]any, 0, len(n.Services))
		for j := range n.Services {
			svcs = append(svcs, s.serviceHealthJSON(r, &n.Services[j], false))
		}
		nodes = append(nodes, map[string]any{
			"device_id":   n.DeviceID,
			"device_name": n.DeviceName,
			"online":      n.Online,
			"services":    svcs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Protocol string `json:"protocol"`
		Port     int    `json:"port"`
		Bind     string `json:"bind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	svc, err := s.Services.Register(r.Context(), services.RegisterRequest{
		UserID:   id.UserID,
		DeviceID: body.DeviceID,
		Name:     body.Name,
		Kind:     body.Kind,
		Protocol: body.Protocol,
		Port:     body.Port,
		Bind:     body.Bind,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "services.create", body.Name, err.Error(), "FAILURE")
		writeServiceErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "services.create", svc.ID, svc.Name, "SUCCESS")
	writeJSON(w, http.StatusCreated, serviceJSON(svc))
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, s.serviceHealthJSON(r, svc, false))
}

func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Name     *string `json:"name"`
		Kind     *string `json:"kind"`
		Protocol *string `json:"protocol"`
		Port     *int    `json:"port"`
		Bind     *string `json:"bind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	svc, err := s.Services.Update(r.Context(), id.UserID, r.PathValue("id"), services.UpdateRequest{
		Name: body.Name, Kind: body.Kind, Protocol: body.Protocol, Port: body.Port, Bind: body.Bind,
	})
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "services.update", svc.ID, svc.Name, "SUCCESS")
	writeJSON(w, http.StatusOK, s.serviceHealthJSON(r, svc, false))
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		apierrors.Write(w, apierrors.Internal("services unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	sid := r.PathValue("id")
	if err := s.Services.Delete(r.Context(), id.UserID, sid); err != nil {
		writeServiceErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "services.delete", sid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func serviceJSON(svc *store.Service) map[string]any {
	listen := fmt.Sprintf("%s://%s:%d", svc.Protocol, svc.Bind, svc.Port)
	return map[string]any{
		"id":            svc.ID,
		"device_id":     svc.DeviceID,
		"device_name":   svc.DeviceName,
		"device_online": svc.DeviceOnline,
		"name":          svc.Name,
		"kind":          svc.Kind,
		"protocol":      svc.Protocol,
		"port":          svc.Port,
		"bind":          svc.Bind,
		"listen":        listen,
		"status":        svc.Status,
		"created_at":    svc.CreatedAt,
		"updated_at":    svc.UpdatedAt,
	}
}

func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, services.ErrConflict):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, services.ErrDevice), errors.Is(err, services.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
