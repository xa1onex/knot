package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/permissions"
)

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "error": "database"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "product": "Node"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"product": "Node"}
	if s.Metrics != nil {
		for k, v := range s.Metrics.Snapshot() {
			out[k] = v
		}
	}
	if !s.StartedAt.IsZero() {
		out["started_at"] = s.StartedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	dbPath := s.Cfg.DatabasePath
	if dbPath == "" {
		apierrors.Write(w, apierrors.Internal("database path unknown"))
		return
	}
	dir := filepath.Join(filepath.Dir(dbPath), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		apierrors.Write(w, apierrors.Internal("backup dir"))
		return
	}
	name := "knot-" + time.Now().UTC().Format("20060102-150405") + ".db"
	dest := filepath.Join(dir, name)
	if err := s.Store.BackupSQLite(r.Context(), dest); err != nil {
		if id != nil && s.Audit != nil {
			s.Audit.Log(r.Context(), id.UserID, id.Actor, "ops.backup", dest, err.Error(), "FAILURE")
		}
		apierrors.Write(w, apierrors.Internal("backup failed"))
		return
	}
	if id != nil && s.Audit != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "ops.backup", dest, "", "SUCCESS")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": dest, "file": name})
}

func (s *Server) handleOpsContext(w http.ResponseWriter, r *http.Request) {
	if s.Ops == nil {
		apierrors.Write(w, apierrors.Internal("ops unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	if id == nil {
		apierrors.Write(w, apierrors.Unauthorized("unauthorized"))
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("service"))
	if name == "" {
		name = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	view, err := s.Ops.Context(r.Context(), ops.ViewRequest{
		UserID:   id.UserID,
		Service:  name,
		DeviceID: strings.TrimSpace(r.URL.Query().Get("device_id")),
		Probe:    r.URL.Query().Get("probe") != "0" && r.URL.Query().Get("probe") != "false",
		Can:      id.Has,
	})
	if err != nil {
		writeOpsErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func writeOpsErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ops.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, ops.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}

func opsContextScopes() []string {
	return []string{
		permissions.ServicesRead, permissions.ReleaseRead, permissions.TrafficRead,
		permissions.DeployRead, permissions.LogsRead,
	}
}
