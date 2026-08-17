package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/knot-infra/knot/internal/store"
	syncjob "github.com/knot-infra/knot/internal/sync"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleCreateSyncJob(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		Name           string `json:"name"`
		Mode           string `json:"mode"`
		SourceDeviceID string `json:"source_device_id"`
		SourcePath     string `json:"source_path"`
		DestDeviceID   string `json:"dest_device_id"`
		DestPath       string `json:"dest_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	j, err := s.Sync.Create(r.Context(), syncjob.CreateRequest{
		UserID:         id.UserID,
		Name:           body.Name,
		Mode:           body.Mode,
		SourceDeviceID: body.SourceDeviceID,
		SourcePath:     body.SourcePath,
		DestDeviceID:   body.DestDeviceID,
		DestPath:       body.DestPath,
	})
	if err != nil {
		writeSyncErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, syncJobJSON(j))
}

func (s *Server) handleListSyncJobs(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Sync.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list sync jobs"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, syncJobJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleGetSyncJob(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	j, err := s.Sync.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		apierrors.Write(w, apierrors.NotFound("sync job not found"))
		return
	}
	writeJSON(w, http.StatusOK, syncJobJSON(j))
}

func (s *Server) handleRunSyncJob(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	j, err := s.Sync.Run(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeSyncErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, syncJobJSON(j))
}

func (s *Server) handleCancelSyncJob(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	j, err := s.Sync.Cancel(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeSyncErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, syncJobJSON(j))
}

func (s *Server) handleDeleteSyncJob(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	if err := s.Sync.Delete(r.Context(), id.UserID, r.PathValue("id")); err != nil {
		apierrors.Write(w, apierrors.Internal("delete failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListSyncFiles(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	jobID := r.PathValue("id")
	if _, err := s.Sync.Get(r.Context(), id.UserID, jobID); err != nil {
		apierrors.Write(w, apierrors.NotFound("sync job not found"))
		return
	}
	list, err := s.Store.ListSyncFileStates(r.Context(), jobID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, f := range list {
		m := map[string]any{
			"rel_path":   f.RelPath,
			"file_id":    f.FileID,
			"size":       f.Size,
			"mtime":      f.Mtime,
			"sha256":     f.SHA256,
			"is_dir":     f.IsDir,
			"deleted":    f.Deleted,
			"created_at": f.CreatedAt,
			"status":     f.Status,
		}
		if f.ConflictID != "" {
			m["conflict_id"] = f.ConflictID
		}
		if f.LastSyncedAt != nil {
			m["last_synced_at"] = f.LastSyncedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

func (s *Server) handleListSyncConflicts(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	jobID := r.PathValue("id")
	j, err := s.Sync.Get(r.Context(), id.UserID, jobID)
	if err != nil {
		apierrors.Write(w, apierrors.NotFound("sync job not found"))
		return
	}
	openOnly := r.URL.Query().Get("open") != "false"
	list, err := s.Store.ListSyncConflicts(r.Context(), jobID, openOnly)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, s.syncConflictJSON(r.Context(), j, &list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conflicts":      out,
		"conflicts_open": j.ConflictsOpen,
	})
}

func (s *Server) handleResolveSyncConflict(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	conflictID := r.PathValue("conflict_id")
	c, err := s.Sync.ResolveConflict(r.Context(), id.UserID, conflictID, body.Resolution)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "sync.conflict.resolve", conflictID, err.Error(), "FAILURE")
		writeSyncErr(w, err)
		return
	}
	j, _ := s.Sync.Get(r.Context(), id.UserID, c.JobID)
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "sync.conflict.resolve", c.ID, c.RelPath+":"+body.Resolution, "SUCCESS")
	writeJSON(w, http.StatusOK, s.syncConflictJSON(r.Context(), j, c))
}

func (s *Server) handleBatchResolveSyncConflicts(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		ConflictIDs []string `json:"conflict_ids"`
		Resolution  string   `json:"resolution"`
		Items       []struct {
			ID         string `json:"id"`
			Resolution string `json:"resolution"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	var items []syncjob.BatchResolveConflictItem
	if len(body.Items) > 0 {
		for _, it := range body.Items {
			items = append(items, syncjob.BatchResolveConflictItem{ID: it.ID, Resolution: it.Resolution})
		}
	} else {
		if body.Resolution == "" || len(body.ConflictIDs) == 0 {
			apierrors.Write(w, apierrors.Validation("conflict_ids+resolution or items required"))
			return
		}
		for _, cid := range body.ConflictIDs {
			items = append(items, syncjob.BatchResolveConflictItem{ID: cid, Resolution: body.Resolution})
		}
	}
	res, err := s.Sync.BatchResolveConflicts(r.Context(), id.UserID, items)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "sync.conflict.batch_resolve", "", err.Error(), "FAILURE")
		writeSyncErr(w, err)
		return
	}
	detail := fmt.Sprintf("resolved=%d errors=%d", len(res.Resolved), len(res.Errors))
	result := "SUCCESS"
	if len(res.Errors) > 0 {
		result = "PARTIAL"
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "sync.conflict.batch_resolve", "", detail, result)
	out := make([]map[string]any, 0, len(res.Resolved))
	for i := range res.Resolved {
		j, _ := s.Sync.Get(r.Context(), id.UserID, res.Resolved[i].JobID)
		out = append(out, s.syncConflictJSON(r.Context(), j, &res.Resolved[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resolved": out,
		"errors":   res.Errors,
	})
}

func syncJobJSON(j *store.SyncJob) map[string]any {
	m := map[string]any{
		"id":                  j.ID,
		"name":                j.Name,
		"mode":                j.Mode,
		"source_device_id":    j.SourceDeviceID,
		"source_path":         j.SourcePath,
		"dest_device_id":      j.DestDeviceID,
		"dest_path":           j.DestPath,
		"status":              j.Status,
		"files_total":         j.FilesTotal,
		"files_done":          j.FilesDone,
		"bytes_total":         j.BytesTotal,
		"bytes_done":          j.BytesDone,
		"current_path":        j.CurrentPath,
		"current_transfer_id": j.CurrentTransferID,
		"last_error":          j.LastError,
		"conflicts_open":      j.ConflictsOpen,
		"created_at":          j.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":          j.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if j.LastRunAt != nil {
		m["last_run_at"] = j.LastRunAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func (s *Server) syncConflictJSON(ctx context.Context, j *store.SyncJob, c *store.SyncConflict) map[string]any {
	m := map[string]any{
		"id":          c.ID,
		"job_id":      c.JobID,
		"rel_path":    c.RelPath,
		"status":      c.Status,
		"a_exists":    c.AExists,
		"a_deleted":   c.ADeleted,
		"a_size":      c.ASize,
		"a_mtime":     c.AMtime,
		"a_sha256":    c.ASHA256,
		"b_exists":    c.BExists,
		"b_deleted":   c.BDeleted,
		"b_size":      c.BSize,
		"b_mtime":     c.BMtime,
		"b_sha256":    c.BSHA256,
		"base_sha256": c.BaseSHA256,
		"base_size":   c.BaseSize,
		"resolution":  c.Resolution,
		"created_at":  c.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if c.ResolvedAt != nil {
		m["resolved_at"] = c.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	if j != nil {
		m["a_device_id"] = j.SourceDeviceID
		m["b_device_id"] = j.DestDeviceID
		m["a_root"] = j.SourcePath
		m["b_root"] = j.DestPath
		aName, bName := "A", "B"
		if d, err := s.Store.GetDeviceByID(ctx, j.SourceDeviceID); err == nil && d.Name != "" {
			aName = d.Name
		}
		if d, err := s.Store.GetDeviceByID(ctx, j.DestDeviceID); err == nil && d.Name != "" {
			bName = d.Name
		}
		m["a_device_name"] = aName
		m["b_device_name"] = bName
		if c.BExists {
			m["keep_both_suggested_name"] = syncjob.ConflictCopyRelPath(c.RelPath, bName, c.BMtime)
		}
	}
	return m
}

func writeSyncErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, syncjob.ErrBusy):
		apierrors.Write(w, apierrors.Conflict(err.Error()))
	case errors.Is(err, syncjob.ErrNotRunning), errors.Is(err, syncjob.ErrBadMode):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Validation(err.Error()))
	}
}

func (s *Server) handleFlushSync(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceID == "" {
		apierrors.Write(w, apierrors.Validation("device_id required"))
		return
	}
	dev, err := s.Store.GetDeviceByID(r.Context(), body.DeviceID)
	if err != nil || dev.UserID != id.UserID {
		apierrors.Write(w, apierrors.NotFound("device not found"))
		return
	}
	res, err := s.Sync.FlushDevice(r.Context(), id.UserID, body.DeviceID)
	if err != nil {
		writeSyncErr(w, err)
		return
	}
	status := http.StatusOK
	if len(res.Errors) > 0 {
		status = http.StatusOK // partial; errors listed in body
	}
	writeJSON(w, status, res)
}

func (s *Server) handleGetFlushSync(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	deviceID := r.PathValue("device_id")
	dev, err := s.Store.GetDeviceByID(r.Context(), deviceID)
	if err != nil || dev.UserID != id.UserID {
		apierrors.Write(w, apierrors.NotFound("device not found"))
		return
	}
	jobs, err := s.Store.ListSyncJobsForDevice(r.Context(), id.UserID, deviceID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("list failed"))
		return
	}
	conflictPaths := []string{}
	jobIDs := []string{}
	for i := range jobs {
		jobIDs = append(jobIDs, jobs[i].ID)
		cs, err := s.Store.ListSyncConflicts(r.Context(), jobs[i].ID, true)
		if err != nil {
			continue
		}
		for _, c := range cs {
			conflictPaths = append(conflictPaths, c.RelPath)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":       deviceID,
		"job_ids":         jobIDs,
		"conflict_paths":  conflictPaths,
		"conflicts_open":  len(conflictPaths),
	})
}
