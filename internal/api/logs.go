package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		apierrors.Write(w, apierrors.Internal("logs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	q := parseLogQuery(r)
	list, err := s.Logs.Query(r.Context(), id.UserID, q)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list logs"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, opsLogJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

func (s *Server) handleIngestLog(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		apierrors.Write(w, apierrors.Internal("logs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		Level        string         `json:"level"`
		Source       string         `json:"source"`
		Message      string         `json:"message"`
		TraceID      string         `json:"trace_id"`
		DeviceID     string         `json:"device_id"`
		ServiceID    string         `json:"service_id"`
		Service      string         `json:"service"`
		ReleaseID    string         `json:"release_id"`
		BuildID      string         `json:"build_id"`
		JobID        string         `json:"job_id"`
		DeploymentID string         `json:"deployment_id"`
		Metadata     map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	src := strings.TrimSpace(body.Source)
	if src == "" {
		src = oplogs.SourceSystem
	}
	if !oplogs.ValidSource(src) {
		apierrors.Write(w, apierrors.Validation("unknown log source"))
		return
	}
	msg := strings.TrimSpace(body.Message)
	if msg == "" {
		apierrors.Write(w, apierrors.Validation("message required"))
		return
	}
	level := strings.ToLower(strings.TrimSpace(body.Level))
	if level == "" {
		level = "info"
	}
	trace := strings.TrimSpace(body.TraceID)
	if trace == "" {
		trace = oplogs.TraceFrom(r.Context())
	}
	ev := oplogs.Event{
		UserID: id.UserID, Level: level, Source: src, Message: msg, TraceID: trace,
		DeviceID: body.DeviceID, ServiceID: body.ServiceID, Service: body.Service,
		ReleaseID: body.ReleaseID, BuildID: body.BuildID, JobID: body.JobID,
		DeploymentID: body.DeploymentID, Metadata: body.Metadata,
	}
	ev = s.Logs.Emit(r.Context(), ev)
	writeJSON(w, http.StatusCreated, opsLogEventJSON(ev))
}

func (s *Server) handleLogsFollow(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		apierrors.Write(w, apierrors.Internal("logs unavailable"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		apierrors.Write(w, apierrors.Internal("streaming unsupported"))
		return
	}
	id := IdentityFrom(r.Context())
	q := parseLogQuery(r)
	if q.Limit <= 0 {
		q.Limit = 50
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	snapshot, err := s.Logs.Query(r.Context(), id.UserID, q)
	if err == nil {
		for i := range snapshot {
			b, _ := json.Marshal(opsLogJSON(&snapshot[i]))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
		}
		fl.Flush()
	}

	ch, cancel := s.Logs.Subscribe(id.UserID, q)
	defer cancel()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(opsLogEventJSON(ev))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		}
	}
}

func parseLogQuery(r *http.Request) oplogs.Query {
	q := r.URL.Query()
	out := oplogs.Query{
		Service:      strings.TrimSpace(q.Get("service")),
		ServiceID:    strings.TrimSpace(q.Get("service_id")),
		ReleaseID:    strings.TrimSpace(q.Get("release_id")),
		BuildID:      strings.TrimSpace(q.Get("build_id")),
		JobID:        strings.TrimSpace(q.Get("job_id")),
		DeploymentID: strings.TrimSpace(q.Get("deployment_id")),
		Source:       strings.TrimSpace(q.Get("source")),
		TraceID:      strings.TrimSpace(q.Get("trace_id")),
		Level:        strings.TrimSpace(q.Get("level")),
		Q:            strings.TrimSpace(q.Get("q")),
		After:        strings.TrimSpace(q.Get("after")),
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil {
		out.Limit = n
	}
	if t := parseLogTime(q.Get("since")); t != nil {
		out.Since = t
	}
	if t := parseLogTime(q.Get("until")); t != nil {
		out.Until = t
	}
	return out
}

func parseLogTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return &t
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	return nil
}

func opsLogJSON(e *store.OpsLog) map[string]any {
	out := map[string]any{
		"id": e.ID, "timestamp": e.CreatedAt, "level": e.Level, "source": e.Source,
		"message": e.Message, "trace_id": e.TraceID, "device_id": e.DeviceID,
		"service_id": e.ServiceID, "service": e.ServiceName, "release_id": e.ReleaseID,
		"build_id": e.BuildID, "job_id": e.JobID, "deployment_id": e.DeploymentID,
	}
	if e.MetadataJSON != "" && e.MetadataJSON != "{}" {
		out["metadata"] = json.RawMessage(e.MetadataJSON)
	}
	return out
}

func opsLogEventJSON(e oplogs.Event) map[string]any {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	out := map[string]any{
		"id": e.ID, "timestamp": ts, "level": e.Level, "source": e.Source,
		"message": e.Message, "trace_id": e.TraceID, "device_id": e.DeviceID,
		"service_id": e.ServiceID, "service": e.Service, "release_id": e.ReleaseID,
		"build_id": e.BuildID, "job_id": e.JobID, "deployment_id": e.DeploymentID,
	}
	if len(e.Metadata) > 0 {
		out["metadata"] = e.Metadata
	}
	return out
}
