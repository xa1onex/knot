package workflows

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

func (s *Service) stepFilesSearch(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Files == nil {
		return nil, fmt.Errorf("files unavailable")
	}
	q := strings.TrimSpace(st.req.Query)
	if q == "" {
		q = "backup"
	}
	dir := false
	hits, err := s.Files.Search(ctx, st.req.UserID, store.FileSearchQuery{
		Query: q, DeviceID: firstNonEmpty(st.req.FromDeviceID, st.req.DeviceID),
		Directories: &dir, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	if path := strings.TrimSpace(st.req.Path); path != "" {
		for i := range hits {
			if hits[i].Path == path {
				st.backup = &hits[i]
				break
			}
		}
	}
	if st.backup == nil {
		st.backup = pickLatestBackup(hits)
	}
	if st.backup == nil {
		return nil, fmt.Errorf("%w: no backup found", ErrValidation)
	}
	return map[string]any{
		"path": st.backup.Path, "device_id": st.backup.DeviceID, "device_name": st.backup.DeviceName,
		"size": st.backup.Size, "mtime": st.backup.Mtime, "hits": len(hits),
	}, nil
}

func (s *Service) stepStorageTransfer(ctx context.Context, st *runState) (map[string]any, error) {
	if st.backup == nil {
		return nil, fmt.Errorf("%w: backup missing", ErrValidation)
	}
	from := st.backup.DeviceID
	to := strings.TrimSpace(st.req.ToDeviceID)
	if to == "" {
		to = strings.TrimSpace(st.req.DeviceID)
	}
	if to == "" || to == from {
		return map[string]any{
			"skipped": true, "reason": "already on target node",
			"device_id": from, "path": st.backup.Path,
		}, nil
	}
	if s.Storage == nil {
		return nil, fmt.Errorf("storage unavailable")
	}
	toPath := strings.TrimSpace(st.req.ToPath)
	if toPath == "" {
		toPath = st.backup.Path
	}
	out, err := s.Storage.TransferBetween(ctx, storage.TransferBetweenRequest{
		UserID: st.req.UserID, CredID: st.req.CredID,
		FromDeviceID: from, FromPath: st.backup.Path, ToDeviceID: to, ToPath: toPath,
	})
	if err != nil {
		return nil, err
	}
	st.backup = &store.FileIndexRow{DeviceID: to, Path: toPath, Name: st.backup.Name, Size: st.backup.Size}
	res := map[string]any{"from": from, "to": to, "path": toPath}
	if out != nil && out.Transfer != nil {
		res["transfer_id"] = out.Transfer.ID
		res["status"] = out.Transfer.Status
	}
	return res, nil
}

func (s *Service) stepJobCreate(ctx context.Context, st *runState) (map[string]any, error) {
	image := strings.TrimSpace(st.req.JobImage)
	if image == "" {
		image = strings.TrimSpace(st.req.Image)
	}
	if image == "" {
		return map[string]any{"skipped": true, "reason": "image not provided (safe mode: locate only)"}, nil
	}
	if s.Jobs == nil {
		return nil, fmt.Errorf("jobs unavailable")
	}
	deviceID := strings.TrimSpace(st.req.ToDeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(st.req.DeviceID)
	}
	if deviceID == "" && st.backup != nil {
		deviceID = st.backup.DeviceID
	}
	input := ""
	if st.backup != nil {
		input = st.backup.Path
	}
	job, err := s.Jobs.Create(ctx, jobs.CreateRequest{
		UserID: st.req.UserID, DeviceID: deviceID, Image: image, InputPath: input,
		CPU: 1, MemoryMB: 256,
	})
	if err != nil {
		return nil, err
	}
	st.job = job
	job, err = s.waitJob(ctx, st.req.UserID, job.ID)
	if job != nil {
		st.job = job
	}
	if err != nil {
		return map[string]any{"job_id": jobID(job), "status": jobStatus(job)}, err
	}
	return map[string]any{"job_id": job.ID, "status": job.Status, "output_path": job.OutputPath}, nil
}

func (s *Service) stepJobArtifacts(ctx context.Context, st *runState) (map[string]any, error) {
	if st.job == nil {
		return map[string]any{"skipped": true, "reason": "no job"}, nil
	}
	if s.Jobs == nil {
		return nil, fmt.Errorf("jobs unavailable")
	}
	arts, err := s.Jobs.Artifacts(ctx, st.req.UserID, st.job.ID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(arts))
	for _, a := range arts {
		names = append(names, a.Path)
	}
	return map[string]any{"job_id": st.job.ID, "status": st.job.Status, "count": len(arts), "paths": names}, nil
}

func (s *Service) waitJob(ctx context.Context, userID, id string) (*store.ComputeJob, error) {
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	var last *store.ComputeJob
	for {
		j, err := s.Jobs.Get(ctx, userID, id)
		if err != nil {
			return last, err
		}
		last = j
		if protocol.JobTerminal(j.Status) {
			if !protocol.JobSucceeded(j.Status) {
				msg := j.Error
				if msg == "" {
					msg = j.Status
				}
				return j, fmt.Errorf("job %s", msg)
			}
			return j, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}

func pickLatestBackup(hits []store.FileIndexRow) *store.FileIndexRow {
	var best *store.FileIndexRow
	for i := range hits {
		h := &hits[i]
		if h.IsDirectory {
			continue
		}
		name := strings.ToLower(h.Name + " " + h.Path)
		if !strings.Contains(name, "backup") && !strings.HasSuffix(strings.ToLower(h.Name), ".zip") {
			continue
		}
		if best == nil || h.Mtime > best.Mtime {
			best = h
		}
	}
	if best != nil {
		return best
	}
	for i := range hits {
		if !hits[i].IsDirectory {
			return &hits[i]
		}
	}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func jobID(j *store.ComputeJob) string {
	if j == nil {
		return ""
	}
	return j.ID
}

func jobStatus(j *store.ComputeJob) string {
	if j == nil {
		return ""
	}
	return j.Status
}
