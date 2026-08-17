package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	if s.Deploy == nil {
		apierrors.Write(w, apierrors.Internal("deploy unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Deploy.List(r.Context(), id.UserID, r.URL.Query().Get("device_id"), r.URL.Query().Get("name"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list deployments"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, deploymentJSON(&list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": out})
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	if s.Deploy == nil {
		apierrors.Write(w, apierrors.Internal("deploy unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID      string            `json:"device_id"`
		Name          string            `json:"name"`
		Image         string            `json:"image"`
		Runtime       string            `json:"runtime"`
		Port          int               `json:"port"`
		Bind          string            `json:"bind"`
		HealthPath    string            `json:"health_path"`
		Env           map[string]string `json:"env"`
		Hostname      string            `json:"hostname"`
		EdgeDeviceID  string            `json:"edge_device_id"`
		Environment   string            `json:"environment"`
		EnvironmentID string            `json:"environment_id"`
		Project       string            `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	dep, err := s.Deploy.Create(r.Context(), deploy.CreateRequest{
		UserID: id.UserID, DeviceID: body.DeviceID, Name: body.Name, Image: body.Image,
		Runtime: body.Runtime, Port: body.Port, Bind: body.Bind, HealthPath: body.HealthPath,
		Env: body.Env, Hostname: body.Hostname, EdgeDeviceID: body.EdgeDeviceID,
		Environment: body.Environment, EnvironmentID: body.EnvironmentID, Project: body.Project,
	})
	auditDetail := body.Name + " " + body.Image
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "deploy.create", body.Name, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeDeployErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "deploy.create", dep.ID, auditDetail, "SUCCESS")
	writeJSON(w, http.StatusCreated, deploymentJSON(dep, true))
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	if s.Deploy == nil {
		apierrors.Write(w, apierrors.Internal("deploy unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	dep, err := s.Deploy.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeDeployErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deploymentJSON(dep, true))
}

func (s *Server) handleDeploymentStop(w http.ResponseWriter, r *http.Request) {
	s.deploymentAction(w, r, "deploy.stop", s.Deploy.Stop)
}

func (s *Server) handleDeploymentRestart(w http.ResponseWriter, r *http.Request) {
	s.deploymentAction(w, r, "deploy.restart", s.Deploy.Restart)
}

func (s *Server) handleDeploymentRollback(w http.ResponseWriter, r *http.Request) {
	s.deploymentAction(w, r, "deploy.rollback", s.Deploy.Rollback)
}

func (s *Server) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	if s.Deploy == nil {
		apierrors.Write(w, apierrors.Internal("deploy unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.Deploy.Logs(r.Context(), id.UserID, r.PathValue("id"), limit)
	if err != nil {
		writeDeployErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		out = append(out, map[string]any{
			"id": l.ID, "stream": l.Stream, "message": l.Message, "created_at": l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

type deployActionFn func(ctx context.Context, userID, depID string) (*store.Deployment, error)

func (s *Server) deploymentAction(w http.ResponseWriter, r *http.Request, auditAction string, fn deployActionFn) {
	if s.Deploy == nil {
		apierrors.Write(w, apierrors.Internal("deploy unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	depID := r.PathValue("id")
	dep, err := fn(r.Context(), id.UserID, depID)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, auditAction, depID, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeDeployErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, auditAction, depID, dep.Name, "SUCCESS")
	writeJSON(w, http.StatusOK, deploymentJSON(dep, true))
}

func deploymentJSON(dep *store.Deployment, includeEnv bool) map[string]any {
	out := map[string]any{
		"id":                 dep.ID,
		"device_id":          dep.DeviceID,
		"device_name":        dep.DeviceName,
		"device_online":      dep.DeviceOnline,
		"service_id":         dep.ServiceID,
		"name":               dep.Name,
		"runtime":            dep.Runtime,
		"image":              dep.Image,
		"port":               dep.Port,
		"bind":               dep.Bind,
		"listen":             dep.Bind + ":" + strconv.Itoa(dep.Port),
		"health_path":        dep.HealthPath,
		"revision":           dep.Revision,
		"status":             dep.Status,
		"container_id":       dep.ContainerID,
		"prev_deployment_id": dep.PrevDeploymentID,
		"active":             dep.Active,
		"health_ok":          dep.HealthOK,
		"error":              dep.Error,
		"environment_id":     dep.EnvironmentID,
		"release_id":         dep.ReleaseID,
		"trace_id":           dep.TraceID,
		"secrets":            deploy.ParseSecretPins(dep.SecretPinsJSON),
		"created_at":         dep.CreatedAt,
		"updated_at":         dep.UpdatedAt,
	}
	if includeEnv {
		out["env"] = deploy.RedactEnv(deploy.ParseEnvJSON(dep.EnvJSON))
	}
	return out
}

func writeDeployErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, deploy.ErrDeviceOffline):
		apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
	case errors.Is(err, deploy.ErrUnhealthy):
		apierrors.WriteCode(w, http.StatusConflict, "unhealthy", err.Error())
	case errors.Is(err, deploy.ErrNothingToRoll):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, deploy.ErrDevice), errors.Is(err, deploy.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, environments.ErrNotFound), errors.Is(err, secrets.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, environments.ErrValidation), errors.Is(err, secrets.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
