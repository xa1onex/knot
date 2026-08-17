package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/compute"
	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrValidation    = errors.New("invalid job")
	ErrNotFound      = errors.New("job not found")
	ErrDevice        = errors.New("device not found")
	ErrDeviceOffline = errors.New("device offline")
	ErrConflict      = errors.New("job already finished")
)

var (
	imageRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,253}(:[a-zA-Z0-9._-]{1,128})?$`)
	envKeyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	shellBins = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "cmd.exe": true, "powershell": true, "pwsh": true}
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Service struct {
	Store   *store.Store
	Sender  Sender
	Compute *compute.Service
	Ops     *oplogs.Service
	mu      sync.Mutex
}

func New(st *store.Store, sender Sender, comp *compute.Service) *Service {
	return &Service{Store: st, Sender: sender, Compute: comp}
}

type CreateRequest struct {
	UserID         string
	DeviceID       string
	Image          string
	Command        []string
	Env            map[string]string
	CPU            float64
	MemoryMB       int64
	GPU            int
	Pids           int64
	DiskMB         int64
	TimeoutSeconds int
	InputPath      string
	OutputPath     string
	Require        map[string]string
	Prefer         map[string]string
	RetryMax       *int
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.ComputeJob, error) {
	if err := normalizeAndValidate(&req); err != nil {
		return nil, err
	}

	cmdJSON, _ := json.Marshal(req.Command)
	if req.Command == nil {
		cmdJSON = []byte("[]")
	}
	envJSON, _ := json.Marshal(req.Env)
	if req.Env == nil {
		envJSON = []byte("{}")
	}

	job := &store.ComputeJob{
		ID: store.NewID(), UserID: req.UserID, DeviceID: req.DeviceID, Image: req.Image,
		CommandJSON: string(cmdJSON), EnvJSON: string(envJSON),
		CPU: req.CPU, MemoryMB: req.MemoryMB, GPU: req.GPU, Pids: req.Pids, DiskMB: req.DiskMB,
		TimeoutSeconds: req.TimeoutSeconds, Status: protocol.JobStatusQueued,
		RequireLabels: labelJSON(req.Require), PreferLabels: labelJSON(req.Prefer),
		SourcePath: req.InputPath,
		TraceID:    oplogs.ResolveTrace(ctx, ""),
	}
	job.InputPath = "jobs/" + job.ID + "/input"
	job.OutputPath = "jobs/" + job.ID + "/output"
	if req.DeviceID != "" {
		dev, err := s.Store.GetDevice(ctx, req.UserID, req.DeviceID)
		if err != nil {
			if store.IsNotFound(err) {
				return nil, ErrDevice
			}
			return nil, err
		}
		if !s.Sender.IsOnline(dev.ID) {
			return nil, ErrDeviceOffline
		}
		job.Placement = protocol.JobPlacementPinned
		job.MaxRetries = 0
		if req.RetryMax != nil && *req.RetryMax >= 0 {
			job.MaxRetries = *req.RetryMax
		}
	} else {
		job.Placement = protocol.JobPlacementScheduled
		job.MaxRetries = protocol.DefaultJobRetries
		if req.RetryMax != nil && *req.RetryMax >= 0 {
			job.MaxRetries = *req.RetryMax
		}
	}
	if err := s.Store.CreateComputeJob(ctx, job); err != nil {
		return nil, err
	}
	s.log(ctx, job.ID, "stdout", fmt.Sprintf("queued image=%s placement=%s", job.Image, job.Placement))

	if job.Placement == protocol.JobPlacementPinned {
		if err := s.dispatch(ctx, job); err != nil {
			return job, err
		}
		return s.Store.GetComputeJob(ctx, req.UserID, job.ID)
	}
	s.Schedule(ctx, req.UserID)
	return s.Store.GetComputeJob(ctx, req.UserID, job.ID)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.ComputeJob, error) {
	j, err := s.Store.GetComputeJob(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return j, nil
}

func (s *Service) List(ctx context.Context, userID, deviceID string) ([]store.ComputeJob, error) {
	return s.Store.ListComputeJobs(ctx, userID, deviceID)
}

func (s *Service) Artifacts(ctx context.Context, userID, id string) ([]store.ComputeJobArtifact, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	return s.Store.ListComputeJobArtifacts(ctx, id)
}

func (s *Service) Logs(ctx context.Context, userID, id string, limit int) ([]store.ComputeJobLog, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	return s.Store.ListComputeJobLogs(ctx, id, limit)
}

func (s *Service) Cancel(ctx context.Context, userID, id string) (*store.ComputeJob, error) {
	j, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if isTerminal(j.Status) {
		return nil, ErrConflict
	}
	if j.Status == protocol.JobStatusQueued || j.Status == protocol.JobStatusWaitingForResource {
		now := time.Now().UTC()
		j.Status = protocol.JobStatusCanceled
		j.Error = "job canceled"
		j.FinishedAt = &now
		_ = s.Store.UpdateComputeJob(ctx, j)
		s.log(ctx, j.ID, "stdout", "canceled before dispatch")
		s.Schedule(ctx, userID)
		return s.Store.GetComputeJob(ctx, userID, j.ID)
	}
	if j.DeviceID == "" || !s.Sender.IsOnline(j.DeviceID) {
		return nil, ErrDeviceOffline
	}
	_ = s.Sender.SendJSON(j.DeviceID, protocol.JobCancel{
		Type: protocol.TypeJobCancel, RequestID: store.NewID(), JobID: j.ID,
	})
	s.log(ctx, j.ID, "stdout", "cancel requested")
	return s.Store.GetComputeJob(ctx, userID, j.ID)
}

func (s *Service) HandleAgentMessage(_ context.Context, _ string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeJobResult:
		var res protocol.JobResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		s.applyResult(res)
	case protocol.TypeJobLogLine:
		var msg protocol.JobLogLine
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		s.log(context.Background(), msg.JobID, msg.Stream, msg.Message)
	}
	return nil
}

func (s *Service) applyResult(res protocol.JobResult) {
	ctx := context.Background()
	j, err := s.Store.GetComputeJobByID(ctx, res.JobID)
	if err != nil {
		return
	}
	if isTerminal(j.Status) {
		return
	}
	now := time.Now().UTC()
	j.Status = res.Status
	j.Reason = res.Reason
	if j.Status == "" {
		if res.OK {
			j.Status = protocol.JobStatusSucceeded
		} else {
			j.Status = protocol.JobStatusFailed
		}
	}
	j.ExitCode = res.ExitCode
	j.Error = deploy.SanitizeLogLine(res.Error)
	j.ContainerID = res.ContainerID
	if res.OutputPath != "" {
		j.OutputPath = res.OutputPath
	}
	j.FinishedAt = &now
	_ = s.Store.UpdateComputeJob(ctx, j)
	if protocol.JobSucceeded(j.Status) {
		s.persistArtifacts(ctx, j, res.Artifacts)
	}
	for _, line := range res.LogLines {
		s.log(ctx, j.ID, "stdout", line)
	}
	s.Schedule(ctx, j.UserID)
}

func (s *Service) persistArtifacts(ctx context.Context, j *store.ComputeJob, arts []protocol.JobArtifact) {
	_ = s.Store.DeleteComputeJobArtifacts(ctx, j.ID)
	for _, a := range arts {
		path := strings.TrimSpace(a.Path)
		if path == "" {
			continue
		}
		meta := &store.StorageFile{
			ID: store.NewID(), UserID: j.UserID, DeviceID: j.DeviceID, Path: path,
			Size: a.Size, SHA256: strings.ToLower(a.SHA256), Status: store.FileComplete, BytesReceived: a.Size,
		}
		if err := s.Store.UpsertStorageFile(ctx, meta); err != nil {
			continue
		}
		name := a.Name
		if name == "" {
			name = filepath.Base(path)
		}
		_ = s.Store.InsertComputeJobArtifact(ctx, &store.ComputeJobArtifact{
			JobID: j.ID, FileID: meta.ID, Path: path, Name: name,
			Size: a.Size, SHA256: strings.ToLower(a.SHA256), MimeType: a.MimeType,
		})
	}
}

func (s *Service) OnDeviceDisconnect(deviceID string) {
	ctx := context.Background()
	list, err := s.Store.ListRunningComputeJobsByDevice(ctx, deviceID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	var userID string
	for i := range list {
		j := &list[i]
		userID = j.UserID
		if j.Placement == protocol.JobPlacementScheduled && j.Attempts <= j.MaxRetries {
			j.Status = protocol.JobStatusWaitingForResource
			j.Reason = protocol.JobReasonWaitingForResource
			j.Error = "agent disconnected; retrying"
			j.DeviceID = ""
			j.StartedAt = nil
			j.FinishedAt = nil
			_ = s.Store.UpdateComputeJob(ctx, j)
			s.log(ctx, j.ID, "stderr", "agent disconnected; queued for retry")
			continue
		}
		j.Status = protocol.JobStatusFailed
		j.Error = "agent disconnected"
		j.FinishedAt = &now
		_ = s.Store.UpdateComputeJob(ctx, j)
		s.log(ctx, j.ID, "stderr", "agent disconnected")
	}
	if userID == "" {
		if d, err := s.Store.GetDeviceByID(ctx, deviceID); err == nil {
			userID = d.UserID
		}
	}
	if userID != "" {
		s.Schedule(ctx, userID)
	}
}

func (s *Service) OnComputeUpdated(userID string) {
	s.Schedule(context.Background(), userID)
}

func (s *Service) dispatch(ctx context.Context, job *store.ComputeJob) error {
	now := time.Now().UTC()
	job.Attempts++
	job.Status = protocol.JobStatusAssigned
	job.StartedAt = &now
	job.FinishedAt = nil
	job.Error = ""
	if err := s.Store.UpdateComputeJob(ctx, job); err != nil {
		return err
	}
	var cmd []string
	_ = json.Unmarshal([]byte(job.CommandJSON), &cmd)
	env := map[string]string{}
	_ = json.Unmarshal([]byte(job.EnvJSON), &env)
	spec := protocol.JobSpec{
		JobID: job.ID, Image: job.Image, Command: cmd, Env: env,
		Resources:      protocol.JobResources{CPU: job.CPU, MemoryMB: job.MemoryMB, GPU: job.GPU, Pids: job.Pids, DiskMB: job.DiskMB},
		TimeoutSeconds: job.TimeoutSeconds, InputPath: job.InputPath, OutputPath: job.OutputPath,
		SourcePath: job.SourcePath,
	}
	if err := s.Sender.SendJSON(job.DeviceID, protocol.JobRun{
		Type: protocol.TypeJobRun, RequestID: store.NewID(), JobID: job.ID, Spec: spec,
	}); err != nil {
		if job.Placement == protocol.JobPlacementScheduled && job.Attempts <= job.MaxRetries {
			job.Status = protocol.JobStatusWaitingForResource
			job.Reason = protocol.JobReasonWaitingForResource
			job.Error = "dispatch failed; retrying"
			job.DeviceID = ""
			job.StartedAt = nil
			job.FinishedAt = nil
			_ = s.Store.UpdateComputeJob(ctx, job)
			s.log(ctx, job.ID, "stderr", "dispatch failed; queued for retry")
			return err
		}
		job.Status = protocol.JobStatusFailed
		job.Error = err.Error()
		fin := time.Now().UTC()
		job.FinishedAt = &fin
		_ = s.Store.UpdateComputeJob(ctx, job)
		return err
	}
	job.Status = protocol.JobStatusRunning
	_ = s.Store.UpdateComputeJob(ctx, job)
	s.log(ctx, job.ID, "stdout", fmt.Sprintf("assigned device=%s attempt=%d", job.DeviceID, job.Attempts))
	return nil
}

func (s *Service) Schedule(ctx context.Context, userID string) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, err := s.Store.ListPendingScheduleJobs(ctx, userID)
	if err != nil || len(pending) == 0 {
		return
	}
	nodes, err := s.scheduleNodes(ctx, userID)
	if err != nil {
		return
	}
	for i := range pending {
		job := pending[i]
		req := scheduleReq{
			CPU: job.CPU, MemoryMB: job.MemoryMB, GPU: job.GPU, DiskMB: job.DiskMB,
			Require: parseLabelMap(job.RequireLabels), Prefer: parseLabelMap(job.PreferLabels),
		}
		id, decision := pickNode(req, nodes)
		switch decision {
		case decisionUnsatisfiable:
			job.Status = protocol.JobStatusRejected
			job.Reason = protocol.JobReasonUnsatisfiable
			job.Error = "unsatisfiable: no node matches CPU/RAM/GPU/disk/labels"
			fin := time.Now().UTC()
			job.FinishedAt = &fin
			_ = s.Store.UpdateComputeJob(ctx, &job)
			s.log(ctx, job.ID, "stderr", job.Error)
		case decisionWait:
			if job.Status != protocol.JobStatusWaitingForResource {
				job.Status = protocol.JobStatusWaitingForResource
				job.Reason = protocol.JobReasonWaitingForResource
				_ = s.Store.UpdateComputeJob(ctx, &job)
				s.log(ctx, job.ID, "stdout", "waiting_for_resource")
			}
		case decisionAssign:
			job.DeviceID = id
			if err := s.dispatch(ctx, &job); err != nil {
				continue
			}
			for j := range nodes {
				if nodes[j].DeviceID == id {
					nodes[j].UsedCPU += job.CPU
					nodes[j].UsedMemMB += job.MemoryMB
					nodes[j].UsedGPU += job.GPU
					nodes[j].UsedDiskMB += job.DiskMB
					break
				}
			}
		}
	}
}

func (s *Service) scheduleNodes(ctx context.Context, userID string) ([]scheduleNode, error) {
	if s.Compute == nil {
		return nil, nil
	}
	recs, err := s.Compute.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	labelRaw, err := s.Store.ListDeviceLabels(ctx, userID)
	if err != nil {
		return nil, err
	}
	inflight, err := s.Store.ListInflightComputeJobs(ctx, userID)
	if err != nil {
		return nil, err
	}
	used := map[string]scheduleNode{}
	for _, j := range inflight {
		u := used[j.DeviceID]
		u.UsedCPU += j.CPU
		u.UsedMemMB += j.MemoryMB
		u.UsedGPU += j.GPU
		u.UsedDiskMB += j.DiskMB
		used[j.DeviceID] = u
	}
	out := make([]scheduleNode, 0, len(recs))
	for _, rec := range recs {
		n := nodeFromRecord(rec, parseLabelMap(labelRaw[rec.DeviceID]))
		if u, ok := used[rec.DeviceID]; ok {
			n.UsedCPU = u.UsedCPU
			n.UsedMemMB = u.UsedMemMB
			n.UsedGPU = u.UsedGPU
			n.UsedDiskMB = u.UsedDiskMB
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) log(ctx context.Context, jobID, stream, msg string) {
	line := deploy.SanitizeLogLine(msg)
	_ = s.Store.AppendComputeJobLog(ctx, jobID, stream, line)
	j, err := s.Store.GetComputeJobByID(ctx, jobID)
	if err != nil {
		return
	}
	s.Ops.Emit(ctx, oplogs.Event{
		UserID: j.UserID, Source: oplogs.SourceJob, Level: oplogs.LevelFromStream(stream),
		Message: line, TraceID: j.TraceID, DeviceID: j.DeviceID, JobID: j.ID,
	})
}

func isTerminal(status string) bool {
	return protocol.JobTerminal(status)
}

func normalizeAndValidate(req *CreateRequest) error {
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Image = strings.TrimSpace(strings.ToLower(req.Image))
	if req.Image == "" || !imageRe.MatchString(req.Image) {
		return fmt.Errorf("%w: invalid image", ErrValidation)
	}
	if err := validateCommand(req.Command); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := validateEnv(req.Env); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if req.CPU <= 0 {
		req.CPU = protocol.DefaultJobCPUs
	}
	if req.CPU > 64 {
		return fmt.Errorf("%w: cpu too large", ErrValidation)
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = protocol.DefaultJobMemoryMB
	}
	if req.MemoryMB < 32 || req.MemoryMB > 256*1024 {
		return fmt.Errorf("%w: memory_mb out of range", ErrValidation)
	}
	if req.GPU < 0 || req.GPU > 8 {
		return fmt.Errorf("%w: gpu out of range", ErrValidation)
	}
	if req.Pids <= 0 {
		req.Pids = protocol.DefaultJobPids
	}
	if req.Pids < protocol.MinJobPids || req.Pids > protocol.MaxJobPids {
		return fmt.Errorf("%w: pids out of range", ErrValidation)
	}
	if req.DiskMB <= 0 {
		req.DiskMB = protocol.DefaultJobDiskMB
	}
	if req.DiskMB < protocol.MinJobDiskMB || req.DiskMB > protocol.MaxJobDiskMB {
		return fmt.Errorf("%w: disk_mb out of range", ErrValidation)
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = protocol.DefaultJobTimeout
	}
	if req.TimeoutSeconds > protocol.MaxJobTimeout {
		return fmt.Errorf("%w: timeout too large", ErrValidation)
	}
	if req.InputPath != "" {
		p, err := cleanStoragePath(req.InputPath)
		if err != nil {
			return fmt.Errorf("%w: input_path", ErrValidation)
		}
		req.InputPath = p
	}
	if req.OutputPath != "" {
		p, err := cleanStoragePath(req.OutputPath)
		if err != nil {
			return fmt.Errorf("%w: output_path", ErrValidation)
		}
		req.OutputPath = p
	}
	if err := validateLabels(req.Require); err != nil {
		return fmt.Errorf("%w: require: %s", ErrValidation, err.Error())
	}
	if err := validateLabels(req.Prefer); err != nil {
		return fmt.Errorf("%w: prefer: %s", ErrValidation, err.Error())
	}
	return nil
}

func validateCommand(cmd []string) error {
	if len(cmd) > protocol.MaxJobArgs {
		return fmt.Errorf("too many command args")
	}
	for _, a := range cmd {
		if a == "" || strings.ContainsRune(a, 0) || len(a) > 4096 {
			return fmt.Errorf("invalid command argument")
		}
	}
	if len(cmd) >= 2 {
		base := strings.ToLower(filepath.Base(cmd[0]))
		if shellBins[base] && (cmd[1] == "-c" || cmd[1] == "/c" || cmd[1] == "-Command") {
			return fmt.Errorf("arbitrary shell is not allowed")
		}
	}
	return nil
}

func validateEnv(env map[string]string) error {
	if len(env) > 32 {
		return fmt.Errorf("too many env vars")
	}
	for k := range env {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("invalid env key")
		}
	}
	return nil
}

func validateLabels(m map[string]string) error {
	return compute.ValidateUserLabels(m)
}

func cleanStoragePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = strings.Trim(p, "/")
	if p == "" || strings.Contains(p, "..") {
		return "", errors.New("bad path")
	}
	return p, nil
}
