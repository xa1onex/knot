package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleFilesSearch(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		apierrors.Write(w, apierrors.Internal("files index unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	minSize, _ := strconv.ParseInt(q.Get("min_size"), 10, 64)
	maxSize, _ := strconv.ParseInt(q.Get("max_size"), 10, 64)
	sq := store.FileSearchQuery{
		Query:          strings.TrimSpace(q.Get("q")),
		DeviceID:       q.Get("device_id"),
		Folder:         strings.Trim(q.Get("folder"), "/"),
		Type:           q.Get("type"),
		MinSize:        minSize,
		MaxSize:        maxSize,
		ModifiedAfter:  q.Get("modified_after"),
		ModifiedBefore: q.Get("modified_before"),
		Limit:          limit,
	}
	if v := q.Get("is_directory"); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		sq.Directories = &b
	}
	rows, err := s.Files.Search(r.Context(), id.UserID, sq)
	if err != nil {
		apierrors.Write(w, apierrors.Internal(err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, fileHitJSON(rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

func (s *Server) handleFilesReindex(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		apierrors.Write(w, apierrors.Internal("files index unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	res, err := s.Files.Reindex(r.Context(), id.UserID, body.DeviceID)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "files.reindex", body.DeviceID, err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "files.reindex", body.DeviceID, "", "SUCCESS")
	writeJSON(w, http.StatusOK, res)
}

func fileHitJSON(r store.FileIndexRow) map[string]any {
	indexed := ""
	if !r.IndexedAt.IsZero() {
		indexed = r.IndexedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":           r.ID,
		"file_id":      r.FileID,
		"device_id":    r.DeviceID,
		"device_name":  r.DeviceName,
		"path":         r.Path,
		"name":         r.Name,
		"size":         r.Size,
		"mtime":        r.Mtime,
		"sha256":       r.SHA256,
		"mime_type":    r.MimeType,
		"is_directory": r.IsDirectory,
		"indexed_at":   indexed,
	}
}
