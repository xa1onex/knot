package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
)

var (
	ErrNotFound    = errors.New("route not found")
	ErrValidation  = errors.New("invalid traffic")
	ErrConflict    = errors.New("traffic conflict")
	ErrNothingToDo = errors.New("no previous traffic target to roll back")
)

type Service struct {
	Store *store.Store
	Logs  *oplogs.Service
}

func New(st *store.Store) *Service {
	return &Service{Store: st}
}

type Status struct {
	RouteID         string
	Hostname        string
	ServiceID       string
	ServiceName     string
	TLSMode         string
	ActiveReleaseID string
	PrevReleaseID   string
	Targets         []Target
	History         []store.RouteTrafficHistory
}

type Target struct {
	ReleaseID string
	Number    int
	Image     string
	Status    string
	Weight    int
	Current   bool
	Port      int
	DeviceID  string
}

type SwitchRequest struct {
	UserID    string
	Actor     string
	Route     string
	ReleaseID string
	Weight    int
}

func (s *Service) ResolveRoute(ctx context.Context, userID, idOrHost string) (*store.EdgeRoute, error) {
	idOrHost = strings.TrimSpace(idOrHost)
	if idOrHost == "" {
		return nil, fmt.Errorf("%w: route id or hostname required", ErrValidation)
	}
	if rt, err := s.Store.GetEdgeRoute(ctx, userID, idOrHost); err == nil {
		return rt, nil
	} else if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	host := edge.NormalizeHost(idOrHost)
	rt, err := s.Store.GetEdgeRouteByHost(ctx, host)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rt.UserID != userID {
		return nil, ErrNotFound
	}
	return rt, nil
}

func (s *Service) Get(ctx context.Context, userID, idOrHost string) (*Status, error) {
	rt, err := s.ResolveRoute(ctx, userID, idOrHost)
	if err != nil {
		return nil, err
	}
	return s.status(ctx, userID, rt)
}

func (s *Service) Switch(ctx context.Context, req SwitchRequest) (*Status, error) {
	rt, err := s.ResolveRoute(ctx, req.UserID, req.Route)
	if err != nil {
		return nil, err
	}
	rel, err := s.Store.GetRelease(ctx, req.UserID, strings.TrimSpace(req.ReleaseID))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, fmt.Errorf("%w: release not found", ErrValidation)
		}
		return nil, err
	}
	if !strings.EqualFold(rel.Service, rt.ServiceName) {
		return nil, fmt.Errorf("%w: release service %s does not match route %s", ErrValidation, rel.Service, rt.ServiceName)
	}
	if rel.Status != store.ReleaseStatusActive {
		return nil, fmt.Errorf("%w: only an active (health-passed) release can receive traffic", ErrConflict)
	}
	if rel.DeviceID == "" || rel.Port < 1 {
		return nil, fmt.Errorf("%w: release has no origin", ErrValidation)
	}
	weight := req.Weight
	if weight <= 0 {
		weight = 100
	}
	if weight > 100 {
		weight = 100
	}

	from := rt.ActiveReleaseID
	targets := []store.RouteTrafficTarget{{ReleaseID: rel.ID, Weight: weight}}
	if from != "" && from != rel.ID {
		rest := 100 - weight
		targets = append(targets, store.RouteTrafficTarget{ReleaseID: from, Weight: rest})
	}
	existing, _ := s.Store.ListRouteTrafficTargets(ctx, rt.ID)
	seen := map[string]bool{rel.ID: true, from: true}
	for _, t := range existing {
		if seen[t.ReleaseID] {
			continue
		}
		seen[t.ReleaseID] = true
		targets = append(targets, store.RouteTrafficTarget{ReleaseID: t.ReleaseID, Weight: 0})
	}
	if err := s.Store.ReplaceRouteTrafficTargets(ctx, rt.ID, targets); err != nil {
		return nil, err
	}
	prev := from
	if from == rel.ID {
		prev = rt.PrevReleaseID
	}
	if err := s.Store.UpdateEdgeRouteBinding(ctx, req.UserID, rt.ID, rel.ID, prev); err != nil {
		return nil, err
	}
	_ = s.Store.AppendRouteTrafficHistory(ctx, &store.RouteTrafficHistory{
		RouteID: rt.ID, Action: "switch", FromReleaseID: from, ToReleaseID: rel.ID,
		WeightsJSON: store.WeightsJSON(targets), CreatedBy: req.Actor,
	})
	rt.ActiveReleaseID = rel.ID
	rt.PrevReleaseID = prev
	s.Logs.Emit(ctx, oplogs.Event{
		UserID: req.UserID, Source: oplogs.SourceSystem, Level: "info",
		Message: fmt.Sprintf("traffic switched %s → %s", rt.Hostname, rel.ID),
		TraceID: rel.TraceID, DeviceID: rel.DeviceID, ServiceID: rt.ServiceID, Service: rt.ServiceName,
		ReleaseID: rel.ID, BuildID: rel.BuildID, JobID: rel.JobID, DeploymentID: rel.DeploymentID,
		Metadata: map[string]any{"action": "switch", "from_release_id": from, "hostname": rt.Hostname},
	})
	return s.status(ctx, req.UserID, rt)
}

func (s *Service) Rollback(ctx context.Context, userID, actor, idOrHost string) (*Status, error) {
	rt, err := s.ResolveRoute(ctx, userID, idOrHost)
	if err != nil {
		return nil, err
	}
	from := rt.ActiveReleaseID
	to := rt.PrevReleaseID
	if to == "" {
		if from == "" {
			return nil, ErrNothingToDo
		}
		if err := s.Store.ReplaceRouteTrafficTargets(ctx, rt.ID, nil); err != nil {
			return nil, err
		}
		if err := s.Store.UpdateEdgeRouteBinding(ctx, userID, rt.ID, "", ""); err != nil {
			return nil, err
		}
		_ = s.Store.AppendRouteTrafficHistory(ctx, &store.RouteTrafficHistory{
			RouteID: rt.ID, Action: "rollback", FromReleaseID: from, ToReleaseID: "",
			WeightsJSON: "{}", CreatedBy: actor,
		})
		rt.ActiveReleaseID = ""
		rt.PrevReleaseID = ""
		s.Logs.Emit(ctx, oplogs.Event{
			UserID: userID, Source: oplogs.SourceSystem, Level: "info",
			Message: fmt.Sprintf("traffic rolled back %s (cleared)", rt.Hostname),
			DeviceID: rt.DeviceID, ServiceID: rt.ServiceID, Service: rt.ServiceName,
			Metadata: map[string]any{"action": "rollback", "from_release_id": from, "hostname": rt.Hostname},
		})
		return s.status(ctx, userID, rt)
	}
	rel, err := s.Store.GetRelease(ctx, userID, to)
	if err != nil {
		return nil, fmt.Errorf("%w: previous release missing", ErrConflict)
	}
	if rel.DeviceID == "" || rel.Port < 1 {
		return nil, fmt.Errorf("%w: previous release has no origin", ErrConflict)
	}
	targets := []store.RouteTrafficTarget{{ReleaseID: to, Weight: 100}}
	if from != "" && from != to {
		targets = append(targets, store.RouteTrafficTarget{ReleaseID: from, Weight: 0})
	}
	if err := s.Store.ReplaceRouteTrafficTargets(ctx, rt.ID, targets); err != nil {
		return nil, err
	}
	if err := s.Store.UpdateEdgeRouteBinding(ctx, userID, rt.ID, to, from); err != nil {
		return nil, err
	}
	_ = s.Store.AppendRouteTrafficHistory(ctx, &store.RouteTrafficHistory{
		RouteID: rt.ID, Action: "rollback", FromReleaseID: from, ToReleaseID: to,
		WeightsJSON: store.WeightsJSON(targets), CreatedBy: actor,
	})
	rt.ActiveReleaseID = to
	rt.PrevReleaseID = from
	s.Logs.Emit(ctx, oplogs.Event{
		UserID: userID, Source: oplogs.SourceSystem, Level: "info",
		Message: fmt.Sprintf("traffic rolled back %s → %s", rt.Hostname, to),
		TraceID: rel.TraceID, DeviceID: rel.DeviceID, ServiceID: rt.ServiceID, Service: rt.ServiceName,
		ReleaseID: rel.ID, BuildID: rel.BuildID, JobID: rel.JobID, DeploymentID: rel.DeploymentID,
		Metadata: map[string]any{"action": "rollback", "from_release_id": from, "hostname": rt.Hostname},
	})
	return s.status(ctx, userID, rt)
}

// OnCandidateReady records a verified release at weight 0 so it can be switched to later.
func (s *Service) OnCandidateReady(ctx context.Context, userID, service, releaseID string) {
	if s == nil || s.Store == nil || releaseID == "" {
		return
	}
	name, err := services.NormalizeDeployName(service)
	if err != nil {
		name = service
	}
	routes, err := s.Store.ListEdgeRoutes(ctx, userID)
	if err != nil {
		return
	}
	for i := range routes {
		if !strings.EqualFold(routes[i].ServiceName, name) {
			continue
		}
		_ = s.Store.UpsertRouteTrafficTarget(ctx, routes[i].ID, releaseID, 0)
	}
}

func (s *Service) status(ctx context.Context, userID string, rt *store.EdgeRoute) (*Status, error) {
	raw, err := s.Store.ListRouteTrafficTargets(ctx, rt.ID)
	if err != nil {
		return nil, err
	}
	hist, err := s.Store.ListRouteTrafficHistory(ctx, rt.ID, 50)
	if err != nil {
		return nil, err
	}
	out := &Status{
		RouteID: rt.ID, Hostname: rt.Hostname, ServiceID: rt.ServiceID, ServiceName: rt.ServiceName,
		TLSMode: rt.TLSMode, ActiveReleaseID: rt.ActiveReleaseID, PrevReleaseID: rt.PrevReleaseID,
		History: hist,
	}
	for _, t := range raw {
		tg := Target{ReleaseID: t.ReleaseID, Weight: t.Weight}
		if rel, err := s.Store.GetRelease(ctx, userID, t.ReleaseID); err == nil && rel != nil {
			tg.Number = rel.Number
			tg.Image = rel.Image
			tg.Status = rel.Status
			tg.Current = rel.Current
			tg.Port = rel.Port
			tg.DeviceID = rel.DeviceID
		}
		out.Targets = append(out.Targets, tg)
	}
	if out.Targets == nil {
		out.Targets = []Target{}
	}
	return out, nil
}
