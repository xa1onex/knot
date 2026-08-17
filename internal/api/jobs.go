package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
)

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	list, err := s.Jobs.List(r.Context(), id.UserID, r.URL.Query().Get("device_id"))
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list jobs"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, jobJSON(&list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID       string            `json:"device_id"`
		Image          string            `json:"image"`
		Command        []string          `json:"command"`
		Env            map[string]string `json:"env"`
		TimeoutSeconds int               `json:"timeout_seconds"`
		InputPath      string            `json:"input_path"`
		OutputPath     string            `json:"output_path"`
		Require        map[string]string `json:"require"`
		Prefer         map[string]string `json:"prefer"`
		RetryMax       *int              `json:"retry_max"`
		Resources      struct {
			CPU      float64         `json:"cpu"`
			MemoryMB int64           `json:"memory_mb"`
			GPU      json.RawMessage `json:"gpu"`
			Pids     int64           `json:"pids"`
			DiskMB   int64           `json:"disk_mb"`
		} `json:"resources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	gpu, err := parseJobGPU(body.Resources.GPU)
	if err != nil {
		apierrors.Write(w, apierrors.Validation("invalid resources.gpu"))
		return
	}
	job, err := s.Jobs.Create(r.Context(), jobs.CreateRequest{
		UserID: id.UserID, DeviceID: body.DeviceID, Image: body.Image,
		Command: body.Command, Env: body.Env,
		CPU: body.Resources.CPU, MemoryMB: body.Resources.MemoryMB, GPU: gpu,
		Pids: body.Resources.Pids, DiskMB: body.Resources.DiskMB,
		TimeoutSeconds: body.TimeoutSeconds, InputPath: body.InputPath, OutputPath: body.OutputPath,
		Require: body.Require, Prefer: body.Prefer, RetryMax: body.RetryMax,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "jobs.create", body.DeviceID, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeJobErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "jobs.create", job.ID, job.Image, "SUCCESS")
	writeJSON(w, http.StatusCreated, jobJSON(job, true))
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	job, err := s.Jobs.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeJobErr(w, err)
		return
	}
	body := jobJSON(job, true)
	if arts, err := s.Jobs.Artifacts(r.Context(), id.UserID, job.ID); err == nil {
		body["artifacts"] = artifactsJSON(arts)
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleJobArtifacts(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	arts, err := s.Jobs.Artifacts(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		writeJobErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifactsJSON(arts)})
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	jobID := r.PathValue("id")
	job, err := s.Jobs.Cancel(r.Context(), id.UserID, jobID)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "jobs.cancel", jobID, deploy.SanitizeLogLine(err.Error()), "FAILURE")
		writeJobErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "jobs.cancel", jobID, job.Image, "SUCCESS")
	writeJSON(w, http.StatusOK, jobJSON(job, true))
}

func (s *Server) handleJobLogs(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		apierrors.Write(w, apierrors.Internal("jobs unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	logs, err := s.Jobs.Logs(r.Context(), id.UserID, r.PathValue("id"), limit)
	if err != nil {
		writeJobErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		out = append(out, map[string]any{
			"id": l.ID, "stream": l.Stream, "message": l.Message, "created_at": l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

func jobJSON(j *store.ComputeJob, includeEnv bool) map[string]any {
	cmd := []string{}
	if j.CommandJSON != "" {
		_ = json.Unmarshal([]byte(j.CommandJSON), &cmd)
		if cmd == nil {
			cmd = []string{}
		}
	}
	out := map[string]any{
		"id":              j.ID,
		"job_id":          j.ID,
		"device_id":       j.DeviceID,
		"device_name":     j.DeviceName,
		"device_online":   j.DeviceOnline,
		"image":           j.Image,
		"command":         cmd,
		"resources":       map[string]any{"cpu": j.CPU, "memory_mb": j.MemoryMB, "gpu": j.GPU, "pids": j.Pids, "disk_mb": j.DiskMB},
		"timeout_seconds": j.TimeoutSeconds,
		"input_path":      j.InputPath,
		"output_path":     j.OutputPath,
		"status":          j.Status,
		"reason":          j.Reason,
		"exit_code":       j.ExitCode,
		"error":           j.Error,
		"container_id":    j.ContainerID,
		"created_at":      j.CreatedAt,
		"started_at":      j.StartedAt,
		"finished_at":     j.FinishedAt,
		"placement":       j.Placement,
		"require":         json.RawMessage(orJSON(j.RequireLabels)),
		"prefer":          json.RawMessage(orJSON(j.PreferLabels)),
		"attempts":        j.Attempts,
		"max_retries":     j.MaxRetries,
		"trace_id":        j.TraceID,
	}
	if includeEnv {
		out["env"] = deploy.RedactEnv(deploy.ParseEnvJSON(j.EnvJSON))
	}
	return out
}

func artifactsJSON(arts []store.ComputeJobArtifact) []map[string]any {
	out := make([]map[string]any, 0, len(arts))
	for _, a := range arts {
		out = append(out, map[string]any{
			"artifact_id": a.ID,
			"job_id":      a.JobID,
			"file_id":     a.FileID,
			"path":        a.Path,
			"name":        a.Name,
			"size":        a.Size,
			"sha256":      a.SHA256,
			"mime_type":   a.MimeType,
			"created_at":  a.CreatedAt,
		})
	}
	return out
}

func orJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

func parseJobGPU(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "required":
			return 1, nil
		case "", "optional", "none":
			return 0, nil
		default:
			return 0, errInvalidGPU
		}
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

var errInvalidGPU = errors.New("invalid gpu")

func writeJobErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		apierrors.Write(w, apierrors.NotFound(err.Error()))
	case errors.Is(err, jobs.ErrDeviceOffline), errors.Is(err, jobs.ErrConflict):
		apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
	case errors.Is(err, jobs.ErrDevice), errors.Is(err, jobs.ErrValidation):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	default:
		apierrors.Write(w, apierrors.Internal(err.Error()))
	}
}
