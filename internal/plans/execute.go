package plans

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/pkg/permissions"
)

func (s *Service) runOpsContext(ctx context.Context, st *runState) (map[string]any, error) {
	if strings.TrimSpace(st.input.Service) == "" {
		return nil, fmt.Errorf("%w: service required", ErrValidation)
	}
	if s.Ops == nil {
		return nil, fmt.Errorf("ops unavailable")
	}
	view, err := s.Ops.Context(ctx, ops.ViewRequest{
		UserID: st.req.UserID, Service: st.input.Service, DeviceID: st.input.DeviceID,
		Probe: true, Can: st.req.Can,
	})
	if err != nil {
		return nil, err
	}
	st.view = view
	out := map[string]any{"service": view.Service, "status": view.Status, "summary": view.Summary}
	if view.CurrentRelease != nil {
		out["current_release"] = view.CurrentRelease.Number
	}
	return out, nil
}

func (s *Service) runTrafficStatus(ctx context.Context, st *runState) (map[string]any, error) {
	host := strings.TrimSpace(st.input.Hostname)
	if host == "" && st.view != nil && st.view.Traffic != nil {
		host = st.view.Traffic.Hostname
	}
	if host == "" && s.Store != nil {
		if rt := routeFor(ctx, s.Store, st.req.UserID, st.input.Service, ""); rt != nil {
			host = rt.Hostname
			st.input.Hostname = host
		}
	}
	if host == "" || s.Traffic == nil {
		return map[string]any{"bound": false}, nil
	}
	tr, err := s.Traffic.Get(ctx, st.req.UserID, host)
	if err != nil {
		return map[string]any{"bound": false, "error": err.Error()}, nil
	}
	st.traffic = tr
	return map[string]any{
		"bound": true, "hostname": tr.Hostname, "active_release_id": tr.ActiveReleaseID,
	}, nil
}

func (s *Service) runReleaseStatus(ctx context.Context, st *runState) (map[string]any, error) {
	name := st.input.Service
	if st.view != nil && st.view.Service != "" {
		name = st.view.Service
	}
	out := map[string]any{}
	if cur, err := s.Store.GetCurrentRelease(ctx, st.req.UserID, name); err == nil {
		st.current = cur
		out["current_id"] = cur.ID
		out["current_number"] = cur.Number
		out["current_status"] = cur.Status
	}
	list, err := s.Store.ListReleases(ctx, st.req.UserID, name)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		latest := list[0]
		st.latest = &latest
		out["latest_id"] = latest.ID
		out["latest_number"] = latest.Number
		out["latest_status"] = latest.Status
	}
	return out, nil
}

func (s *Service) runLogsSearch(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Logs == nil {
		return map[string]any{"recent_errors": 0}, nil
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	list, err := s.Logs.Query(ctx, st.req.UserID, oplogs.Query{
		Service: st.input.Service, Level: "error", Since: &since, Limit: 20,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"recent_errors": len(list), "trace_id": oplogs.TraceFrom(ctx)}, nil
}

func (s *Service) runHealthCheck(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Store == nil || s.Edge == nil || !st.req.Can(permissions.ServicesRead) {
		return map[string]any{"checked": false}, nil
	}
	svc, err := s.Store.FindServiceByName(ctx, st.req.UserID, st.input.Service)
	if err != nil {
		if store.IsNotFound(err) {
			return map[string]any{"registered": false}, nil
		}
		return nil, err
	}
	h := s.Edge.Health(ctx, svc, true)
	return map[string]any{
		"registered": h.Registered, "agent_online": h.AgentOnline, "backend_reachable": h.BackendReachable,
	}, nil
}

func (s *Service) runBuildStatus(ctx context.Context, st *runState) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(st.input.BuildID) != "" && s.Builds != nil {
		b, err := s.Builds.Get(ctx, st.req.UserID, st.input.BuildID)
		if err != nil {
			return nil, err
		}
		out["build_id"] = b.ID
		out["status"] = b.Status
		out["image"] = b.Image
		if st.input.Image == "" {
			st.input.Image = b.Image
		}
		return out, nil
	}
	if strings.TrimSpace(st.input.Image) != "" {
		return map[string]any{"status": "using_image", "image": st.input.Image}, nil
	}
	return map[string]any{"status": "none"}, nil
}

func (s *Service) runReleaseCreate(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Releases == nil {
		return nil, fmt.Errorf("releases unavailable")
	}
	if strings.TrimSpace(st.input.Service) == "" {
		return nil, fmt.Errorf("%w: service required", ErrValidation)
	}
	if strings.TrimSpace(st.input.Image) == "" {
		return nil, fmt.Errorf("%w: image or completed build required", ErrValidation)
	}
	rel, err := s.Releases.Create(ctx, releases.CreateRequest{
		UserID: st.req.UserID, CreatedBy: st.req.Actor, Service: st.input.Service,
		Image: st.input.Image, Environment: st.input.Environment, DeviceID: st.input.DeviceID,
		Port: st.input.Port, Hostname: st.input.Hostname, BuildID: st.input.BuildID,
	})
	if err != nil {
		return nil, err
	}
	st.created = rel
	return map[string]any{"release_id": rel.ID, "number": rel.Number, "status": rel.Status, "image": rel.Image}, nil
}

func (s *Service) runDeploy(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Releases == nil || st.created == nil {
		return nil, fmt.Errorf("%w: release missing", ErrValidation)
	}
	rel, err := s.Releases.Deploy(ctx, releases.DeployRequest{
		UserID: st.req.UserID, ID: st.created.ID, DeviceID: st.input.DeviceID, Port: st.input.Port,
	})
	if rel != nil {
		st.created = rel
	}
	if err != nil {
		return map[string]any{"release_id": st.created.ID, "status": st.created.Status, "error": err.Error()}, err
	}
	return map[string]any{
		"release_id": rel.ID, "number": rel.Number, "status": rel.Status, "health_ok": rel.Status == store.ReleaseStatusActive,
	}, nil
}

func (s *Service) runHealthGate(ctx context.Context, st *runState) (map[string]any, error) {
	if st.created == nil {
		return nil, fmt.Errorf("%w: release missing", ErrValidation)
	}
	rel := st.created
	if s.Releases != nil {
		if got, err := s.Releases.Get(ctx, st.req.UserID, st.created.ID); err == nil {
			rel = got
			st.created = got
		}
	}
	out := map[string]any{"release_id": rel.ID, "number": rel.Number, "status": rel.Status, "traffic_switched": false}
	if rel.Status == store.ReleaseStatusFailed {
		return out, fmt.Errorf("health gate failed: %s", rel.Error)
	}
	return out, nil
}

func (s *Service) runTrafficSwitch(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Traffic == nil || st.created == nil {
		return nil, fmt.Errorf("%w: traffic target missing", ErrValidation)
	}
	host := strings.TrimSpace(st.input.Hostname)
	if host == "" {
		if rt := routeFor(ctx, s.Store, st.req.UserID, st.input.Service, ""); rt != nil {
			host = rt.Hostname
		}
	}
	if host == "" {
		return nil, fmt.Errorf("%w: hostname required for traffic.switch", ErrValidation)
	}
	tr, err := s.Traffic.Switch(ctx, traffic.SwitchRequest{
		UserID: st.req.UserID, Actor: st.req.Actor, Route: host,
		ReleaseID: st.created.ID, Weight: 100,
	})
	if err != nil {
		return nil, err
	}
	st.traffic = tr
	return map[string]any{
		"hostname": tr.Hostname, "route_id": tr.RouteID, "release_id": tr.ActiveReleaseID, "weight": 100,
	}, nil
}

func (s *Service) runFilesSearch(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Files == nil {
		return nil, fmt.Errorf("files unavailable")
	}
	q := strings.TrimSpace(st.input.Query)
	if q == "" {
		q = "backup"
	}
	dir := false
	hits, err := s.Files.Search(ctx, st.req.UserID, store.FileSearchQuery{
		Query: q, DeviceID: firstNonEmpty(st.input.FromDeviceID, st.input.DeviceID),
		Directories: &dir, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("%w: no backup found", ErrValidation)
	}
	st.backup = &hits[0]
	return map[string]any{"path": st.backup.Path, "device_id": st.backup.DeviceID, "hits": len(hits)}, nil
}

func (s *Service) runStorageTransfer(ctx context.Context, st *runState) (map[string]any, error) {
	if st.backup == nil {
		return nil, fmt.Errorf("%w: backup missing", ErrValidation)
	}
	to := firstNonEmpty(st.input.ToDeviceID, st.input.DeviceID)
	if to == "" || to == st.backup.DeviceID {
		return map[string]any{"skipped": true, "reason": "already on target node", "path": st.backup.Path}, nil
	}
	if s.Storage == nil {
		return nil, fmt.Errorf("storage unavailable")
	}
	toPath := st.input.ToPath
	if toPath == "" {
		toPath = st.backup.Path
	}
	out, err := s.Storage.TransferBetween(ctx, storage.TransferBetweenRequest{
		UserID: st.req.UserID, CredID: st.req.CredID,
		FromDeviceID: st.backup.DeviceID, FromPath: st.backup.Path, ToDeviceID: to, ToPath: toPath,
	})
	if err != nil {
		return nil, err
	}
	res := map[string]any{"from": st.backup.DeviceID, "to": to, "path": toPath}
	if out != nil && out.Transfer != nil {
		res["transfer_id"] = out.Transfer.ID
	}
	st.backup = &store.FileIndexRow{DeviceID: to, Path: toPath, Size: st.backup.Size}
	return res, nil
}

func (s *Service) runJobCreate(ctx context.Context, st *runState) (map[string]any, error) {
	image := firstNonEmpty(st.input.JobImage, st.input.Image)
	if image == "" {
		return map[string]any{"skipped": true, "reason": "image not provided"}, nil
	}
	if s.Jobs == nil {
		return nil, fmt.Errorf("jobs unavailable")
	}
	deviceID := firstNonEmpty(st.input.ToDeviceID, st.input.DeviceID)
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
	return map[string]any{"job_id": job.ID, "status": job.Status, "image": job.Image}, nil
}

func (s *Service) runJobArtifacts(ctx context.Context, st *runState) (map[string]any, error) {
	if st.job == nil {
		return map[string]any{"skipped": true}, nil
	}
	if s.Jobs == nil {
		return map[string]any{"job_id": st.job.ID}, nil
	}
	arts, err := s.Jobs.Artifacts(ctx, st.req.UserID, st.job.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"job_id": st.job.ID, "artifacts": len(arts)}, nil
}

func routeFor(ctx context.Context, st *store.Store, userID, name, serviceID string) *store.EdgeRoute {
	if st == nil {
		return nil
	}
	if serviceID != "" {
		if list, err := st.ListEdgeRoutesByService(ctx, userID, serviceID); err == nil && len(list) > 0 {
			return &list[0]
		}
	}
	list, err := st.ListEdgeRoutes(ctx, userID)
	if err != nil {
		return nil
	}
	for i := range list {
		if strings.EqualFold(list[i].ServiceName, name) {
			return &list[i]
		}
	}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
