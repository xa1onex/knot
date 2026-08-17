package workflows

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/permissions"
)

func (s *Service) stepOpsContext(ctx context.Context, st *runState) (map[string]any, error) {
	if strings.TrimSpace(st.req.Service) == "" {
		return nil, fmt.Errorf("%w: service required", ErrValidation)
	}
	if s.Ops == nil {
		return nil, fmt.Errorf("ops unavailable")
	}
	view, err := s.Ops.Context(ctx, ops.ViewRequest{
		UserID: st.req.UserID, Service: st.req.Service, DeviceID: st.req.DeviceID,
		Probe: true, Can: st.req.Can,
	})
	if err != nil {
		return nil, err
	}
	st.view = view
	out := map[string]any{
		"service": view.Service, "status": view.Status, "node": view.Node,
		"environment": view.Environment, "trace_id": view.TraceID, "summary": view.Summary,
		"recent_errors": view.RecentErrors,
	}
	if view.CurrentRelease != nil {
		out["current_release"] = view.CurrentRelease.Number
		out["current_release_id"] = view.CurrentRelease.ID
	}
	if view.LatestRelease != nil {
		out["latest_release"] = view.LatestRelease.Number
		out["latest_status"] = view.LatestRelease.Status
	}
	if view.Traffic != nil {
		out["traffic_weight"] = view.Traffic.Weight
		out["hostname"] = view.Traffic.Hostname
	}
	return out, nil
}

func (s *Service) stepTrafficStatus(ctx context.Context, st *runState) (map[string]any, error) {
	host := strings.TrimSpace(st.req.Hostname)
	if host == "" && st.view != nil && st.view.Traffic != nil {
		host = st.view.Traffic.Hostname
	}
	if host == "" && s.Store != nil {
		if rt := routeFor(ctx, s.Store, st.req.UserID, st.req.Service, ""); rt != nil {
			host = rt.Hostname
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
	out := map[string]any{
		"bound": true, "hostname": tr.Hostname, "route_id": tr.RouteID,
		"active_release_id": tr.ActiveReleaseID, "prev_release_id": tr.PrevReleaseID,
	}
	for _, t := range tr.Targets {
		if t.ReleaseID == tr.ActiveReleaseID {
			out["weight"] = t.Weight
			out["active_number"] = t.Number
			break
		}
	}
	return out, nil
}

func (s *Service) stepReleaseStatus(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	name := st.req.Service
	if st.view != nil && st.view.Service != "" {
		name = st.view.Service
	}
	cur, err := s.Store.GetCurrentRelease(ctx, st.req.UserID, name)
	if err == nil {
		st.current = cur
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	list, err := s.Store.ListReleases(ctx, st.req.UserID, name)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		latest := list[0]
		st.latest = &latest
	}
	out := map[string]any{}
	if st.current != nil {
		out["current_id"] = st.current.ID
		out["current_number"] = st.current.Number
		out["current_status"] = st.current.Status
		out["current_image"] = st.current.Image
	}
	if st.latest != nil {
		out["latest_id"] = st.latest.ID
		out["latest_number"] = st.latest.Number
		out["latest_status"] = st.latest.Status
		out["latest_error"] = st.latest.Error
	}
	return out, nil
}

func (s *Service) stepLogsSearch(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Logs == nil {
		return map[string]any{"recent_errors": 0}, nil
	}
	name := st.req.Service
	since := time.Now().UTC().Add(-24 * time.Hour)
	list, err := s.Logs.Query(ctx, st.req.UserID, oplogs.Query{
		Service: name, Level: "error", Since: &since, Limit: 20,
	})
	if err != nil {
		return nil, err
	}
	msgs := make([]string, 0, len(list))
	for i, ev := range list {
		if i >= 5 {
			break
		}
		msgs = append(msgs, ev.Message)
	}
	return map[string]any{"recent_errors": len(list), "samples": msgs, "trace_id": oplogs.TraceFrom(ctx)}, nil
}

func (s *Service) stepHealthCheck(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Store == nil || s.Edge == nil {
		return map[string]any{"checked": false}, nil
	}
	if !st.req.Can(permissions.ServicesRead) {
		return map[string]any{"checked": false}, nil
	}
	svc, err := s.Store.FindServiceByName(ctx, st.req.UserID, st.req.Service)
	if err != nil {
		if store.IsNotFound(err) {
			return map[string]any{"registered": false}, nil
		}
		return nil, err
	}
	h := s.Edge.Health(ctx, svc, true)
	out := map[string]any{
		"registered": h.Registered, "agent_online": h.AgentOnline,
		"backend_reachable": h.BackendReachable, "edge_online": h.EdgeOnline,
	}
	if h.Error != "" {
		out["error"] = h.Error
	}
	return out, nil
}

func diagnoseResult(st *runState) map[string]any {
	out := map[string]any{
		"cause":          "no incident detected",
		"traffic":        "n/a",
		"recommendation": "none",
		"status":         "unknown",
	}
	if st.view != nil {
		out["status"] = st.view.Status
		out["summary"] = st.view.Summary
	}
	cur, latest := st.current, st.latest
	if cur == nil && st.view != nil && st.view.CurrentRelease != nil {
		cur = &store.Release{ID: st.view.CurrentRelease.ID, Number: st.view.CurrentRelease.Number, Status: st.view.CurrentRelease.Status}
	}
	if latest == nil && st.view != nil && st.view.LatestRelease != nil {
		latest = &store.Release{ID: st.view.LatestRelease.ID, Number: st.view.LatestRelease.Number, Status: st.view.LatestRelease.Status}
	}
	if latest != nil && cur != nil && latest.ID != cur.ID && latest.Status == store.ReleaseStatusFailed {
		out["cause"] = fmt.Sprintf("release #%d failed health", latest.Number)
		out["traffic"] = fmt.Sprintf("still on #%d", cur.Number)
		out["recommendation"] = "rollback not required"
		out["status"] = "degraded"
		return out
	}
	if cur != nil && st.traffic != nil && st.traffic.ActiveReleaseID != "" && st.traffic.ActiveReleaseID == cur.ID {
		out["traffic"] = fmt.Sprintf("100%% on #%d", cur.Number)
	} else if st.view != nil && st.view.Traffic != nil && cur != nil {
		out["traffic"] = fmt.Sprintf("%d%% on #%d", st.view.Traffic.Weight, cur.Number)
	}
	if st.view != nil && (st.view.Status == "down" || st.view.Status == "unhealthy") {
		out["cause"] = "service health failed"
		out["recommendation"] = "inspect logs and last deploy"
	}
	return out
}

func routeFor(ctx context.Context, st *store.Store, userID, name, serviceID string) *store.EdgeRoute {
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
