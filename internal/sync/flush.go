package syncjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/knot-infra/knot/internal/store"
)

// FlushResult is returned after reconnect drain for a device.
type FlushResult struct {
	DeviceID      string   `json:"device_id"`
	JobIDs        []string `json:"job_ids"`
	ConflictPaths []string `json:"conflict_paths"`
	Errors        []string `json:"errors,omitempty"`
}

// FlushDevice runs sync jobs that include deviceID and waits for each to settle.
// Prefer two_way jobs; one_way jobs are run only when the device is the source.
// Conflicts use the existing sync_conflicts model — no separate offline conflict system.
func (s *Service) FlushDevice(ctx context.Context, userID, deviceID string) (*FlushResult, error) {
	jobIDs, conflictPaths, errs, err := s.flushDevice(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	return &FlushResult{
		DeviceID:      deviceID,
		JobIDs:        jobIDs,
		ConflictPaths: conflictPaths,
		Errors:        errs,
	}, nil
}

// HubFlush adapts Service to agentws.OfflineFlusher.
type HubFlush struct {
	S *Service
}

func (h HubFlush) FlushDevice(ctx context.Context, userID, deviceID string) (jobIDs, conflictPaths []string, errs []string, err error) {
	return h.S.flushDevice(ctx, userID, deviceID)
}

func (s *Service) flushDevice(ctx context.Context, userID, deviceID string) (jobIDs, conflictPaths []string, errs []string, err error) {
	if deviceID == "" {
		return nil, nil, nil, fmt.Errorf("device_id required")
	}
	jobs, err := s.Store.ListSyncJobsForDevice(ctx, userID, deviceID)
	if err != nil {
		return nil, nil, nil, err
	}
	conflictSet := map[string]struct{}{}

	for i := range jobs {
		j := jobs[i]
		if j.Mode == store.SyncModeOneWay && j.SourceDeviceID != deviceID {
			continue
		}
		if _, runErr := s.Run(ctx, userID, j.ID); runErr != nil {
			if !errors.Is(runErr, ErrBusy) {
				errs = append(errs, fmt.Sprintf("%s: %v", j.ID, runErr))
				continue
			}
		}
		settled, waitErr := s.waitSettled(ctx, userID, j.ID)
		if waitErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", j.ID, waitErr))
			continue
		}
		jobIDs = append(jobIDs, settled.ID)
		if settled.Status == store.SyncFailed {
			errs = append(errs, fmt.Sprintf("%s: %s", settled.ID, settled.LastError))
		}
		conflicts, cErr := s.Store.ListSyncConflicts(ctx, settled.ID, true)
		if cErr != nil {
			errs = append(errs, fmt.Sprintf("%s conflicts: %v", settled.ID, cErr))
			continue
		}
		for _, c := range conflicts {
			conflictSet[c.RelPath] = struct{}{}
		}
	}
	for p := range conflictSet {
		conflictPaths = append(conflictPaths, p)
	}
	return jobIDs, conflictPaths, errs, nil
}

func (s *Service) waitSettled(ctx context.Context, userID, jobID string) (*store.SyncJob, error) {
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		j, err := s.Store.GetSyncJob(ctx, userID, jobID)
		if err != nil {
			return nil, err
		}
		switch j.Status {
		case store.SyncRunning, store.SyncCanceling:
		default:
			return j, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
