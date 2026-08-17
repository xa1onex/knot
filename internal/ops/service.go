package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/pkg/permissions"
)

var (
	ErrNotFound   = errors.New("service not found")
	ErrValidation = errors.New("invalid ops context")
)

// Service assembles a read-only operational snapshot from existing primitives.
// It does not mutate state and does not introduce an AI permission layer.
type Service struct {
	Store   *store.Store
	Edge    *edge.Proxy
	Traffic *traffic.Service
	Logs    *oplogs.Service
}

func New(st *store.Store, edgeProxy *edge.Proxy, traf *traffic.Service, logs *oplogs.Service) *Service {
	return &Service{Store: st, Edge: edgeProxy, Traffic: traf, Logs: logs}
}

type ViewRequest struct {
	UserID   string
	Service  string
	DeviceID string
	Probe    bool
	Can      func(scope string) bool
}

type ReleaseInfo struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type TrafficInfo struct {
	Hostname        string `json:"hostname"`
	RouteID         string `json:"route_id"`
	ActiveReleaseID string `json:"active_release_id"`
	PrevReleaseID   string `json:"prev_release_id"`
	Weight          int    `json:"weight"`
}

type DeployInfo struct {
	ID        string    `json:"id"`
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
	HealthOK  bool      `json:"health_ok"`
	CreatedAt time.Time `json:"created_at"`
}

type Context struct {
	Service          string         `json:"service"`
	ServiceID        string         `json:"service_id,omitempty"`
	Node             string         `json:"node,omitempty"`
	NodeID           string         `json:"node_id,omitempty"`
	Status           string         `json:"status"`
	Environment      string         `json:"environment,omitempty"`
	CurrentRelease   *ReleaseInfo   `json:"current_release,omitempty"`
	LatestRelease    *ReleaseInfo   `json:"latest_release,omitempty"`
	Traffic          *TrafficInfo   `json:"traffic,omitempty"`
	LastDeploy       *DeployInfo    `json:"last_deploy,omitempty"`
	LastDeployAt     *time.Time     `json:"last_deploy_at,omitempty"`
	RecentErrors     int            `json:"recent_errors"`
	Health           map[string]any `json:"health,omitempty"`
	TraceID          string         `json:"trace_id,omitempty"`
	Visible          []string       `json:"visible"`
	Summary          string         `json:"summary"`
}

func (s *Service) Context(ctx context.Context, req ViewRequest) (*Context, error) {
	name := strings.TrimSpace(req.Service)
	if name == "" {
		return nil, fmt.Errorf("%w: service required", ErrValidation)
	}
	if n, err := services.NormalizeDeployName(name); err == nil {
		name = n
	}
	can := req.Can
	if can == nil {
		can = func(string) bool { return true }
	}

	out := &Context{Status: "unknown", Visible: []string{}, RecentErrors: 0}
	found := false

	var svc *store.Service
	if s.Store != nil {
		if rec, err := s.Store.GetService(ctx, req.UserID, strings.TrimSpace(req.Service)); err == nil {
			svc = rec
			name = rec.Name
			found = true
		} else if req.DeviceID != "" {
			if rec, err := s.Store.GetServiceByName(ctx, req.UserID, req.DeviceID, name); err == nil {
				svc = rec
				found = true
			}
		} else if rec, err := s.Store.FindServiceByName(ctx, req.UserID, name); err == nil {
			svc = rec
			found = true
		}
	}

	out.Service = name
	if svc != nil && can(permissions.ServicesRead) {
		out.ServiceID = svc.ID
		out.Node = svc.DeviceName
		out.NodeID = svc.DeviceID
		out.Visible = append(out.Visible, "service")
		if s.Edge != nil {
			h := s.Edge.Health(ctx, svc, req.Probe)
			out.Health = map[string]any{
				"registered":        h.Registered,
				"agent_online":      h.AgentOnline,
				"tunnel_connected":  h.TunnelConnected,
				"backend_reachable": h.BackendReachable,
				"edge_online":       h.EdgeOnline,
				"hostnames":         h.Hostnames,
			}
			if h.Error != "" {
				out.Health["error"] = h.Error
			}
			out.Visible = append(out.Visible, "health")
			if h.BackendReachable {
				out.Status = "healthy"
			} else if h.AgentOnline {
				out.Status = "degraded"
			} else {
				out.Status = "down"
			}
		}
	}

	if can(permissions.ReleaseRead) && s.Store != nil {
		out.Visible = append(out.Visible, "release")
		if cur, err := s.Store.GetCurrentRelease(ctx, req.UserID, name); err == nil && cur != nil {
			found = true
			out.CurrentRelease = releaseInfo(cur)
			out.Environment = cur.EnvironmentName
			out.TraceID = cur.TraceID
			if out.Node == "" {
				out.Node = cur.DeviceName
				out.NodeID = cur.DeviceID
			}
		}
		if list, err := s.Store.ListReleases(ctx, req.UserID, name); err == nil && len(list) > 0 {
			found = true
			latest := list[0]
			out.LatestRelease = releaseInfo(&latest)
			if out.TraceID == "" {
				out.TraceID = latest.TraceID
			}
			if out.Node == "" {
				out.Node = latest.DeviceName
				out.NodeID = latest.DeviceID
			}
			if out.Environment == "" {
				out.Environment = latest.EnvironmentName
			}
			if out.CurrentRelease != nil && latest.ID != out.CurrentRelease.ID && latest.Status == store.ReleaseStatusFailed {
				out.Status = "degraded"
			} else if out.CurrentRelease == nil && latest.Status == store.ReleaseStatusFailed {
				out.Status = "unhealthy"
			} else if out.CurrentRelease != nil && out.CurrentRelease.Status == store.ReleaseStatusActive && out.Status == "unknown" {
				out.Status = "healthy"
			}
		}
	}

	if can(permissions.TrafficRead) && s.Store != nil {
		rt := s.routeForService(ctx, req.UserID, name, out.ServiceID)
		if rt != nil {
			found = true
			out.Visible = append(out.Visible, "traffic")
			info := &TrafficInfo{
				Hostname: rt.Hostname, RouteID: rt.ID,
				ActiveReleaseID: rt.ActiveReleaseID, PrevReleaseID: rt.PrevReleaseID,
			}
			info.Weight = 0
			if s.Traffic != nil {
				if st, err := s.Traffic.Get(ctx, req.UserID, rt.ID); err == nil {
					for _, t := range st.Targets {
						if t.ReleaseID == st.ActiveReleaseID {
							info.Weight = t.Weight
							break
						}
					}
				}
			}
			if info.Weight == 0 && rt.ActiveReleaseID != "" {
				info.Weight = 100
			}
			out.Traffic = info
		}
	}

	if can(permissions.DeployRead) && s.Store != nil {
		out.Visible = append(out.Visible, "deploy")
		deviceID := req.DeviceID
		if deviceID == "" {
			deviceID = out.NodeID
		}
		if list, err := s.Store.ListDeployments(ctx, req.UserID, deviceID, name); err == nil && len(list) > 0 {
			found = true
			d := list[0]
			out.LastDeploy = &DeployInfo{
				ID: d.ID, Revision: d.Revision, Status: d.Status, HealthOK: d.HealthOK, CreatedAt: d.CreatedAt,
			}
			t := d.CreatedAt
			out.LastDeployAt = &t
		}
	}

	if can(permissions.LogsRead) && s.Logs != nil {
		out.Visible = append(out.Visible, "logs")
		since := time.Now().UTC().Add(-24 * time.Hour)
		if list, err := s.Logs.Query(ctx, req.UserID, oplogs.Query{
			Service: name, Level: "error", Since: &since, Limit: 200,
		}); err == nil {
			out.RecentErrors = len(list)
			if len(list) > 0 {
				found = true
			}
		}
	}

	if !found {
		return nil, ErrNotFound
	}
	if out.Status == "unknown" && out.RecentErrors > 0 {
		out.Status = "degraded"
	}
	out.Summary = formatSummary(out)
	return out, nil
}

func (s *Service) routeForService(ctx context.Context, userID, name, serviceID string) *store.EdgeRoute {
	if serviceID != "" {
		if list, err := s.Store.ListEdgeRoutesByService(ctx, userID, serviceID); err == nil && len(list) > 0 {
			return &list[0]
		}
	}
	list, err := s.Store.ListEdgeRoutes(ctx, userID)
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

func releaseInfo(rel *store.Release) *ReleaseInfo {
	if rel == nil {
		return nil
	}
	return &ReleaseInfo{ID: rel.ID, Number: rel.Number, Image: rel.Image, Status: rel.Status}
}

func formatSummary(c *Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Service: %s\n", c.Service)
	if c.CurrentRelease != nil {
		fmt.Fprintf(&b, "Current Release: #%d (%s)\n", c.CurrentRelease.Number, c.CurrentRelease.Status)
	} else {
		b.WriteString("Current Release: none\n")
	}
	if c.LatestRelease != nil && (c.CurrentRelease == nil || c.LatestRelease.ID != c.CurrentRelease.ID) {
		fmt.Fprintf(&b, "Latest Release: #%d (%s)\n", c.LatestRelease.Number, c.LatestRelease.Status)
	}
	env := c.Environment
	if env == "" {
		env = "none"
	}
	fmt.Fprintf(&b, "Environment: %s\n", env)
	node := c.Node
	if node == "" {
		node = "unknown"
	}
	fmt.Fprintf(&b, "Node: %s\n", node)
	fmt.Fprintf(&b, "Status: %s\n", c.Status)
	if c.Traffic != nil {
		fmt.Fprintf(&b, "Traffic: %d%% on %s\n", c.Traffic.Weight, c.Traffic.Hostname)
	} else {
		b.WriteString("Traffic: n/a\n")
	}
	if c.LastDeployAt != nil {
		fmt.Fprintf(&b, "Last deploy: %s\n", ago(*c.LastDeployAt))
	} else {
		b.WriteString("Last deploy: n/a\n")
	}
	fmt.Fprintf(&b, "Recent errors: %d\n", c.RecentErrors)
	return strings.TrimRight(b.String(), "\n")
}

func ago(t time.Time) string {
	d := time.Since(t.UTC())
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		n := int(d.Minutes())
		if n == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", n)
	}
	if d < 24*time.Hour {
		n := int(d.Hours())
		if n == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", n)
	}
	n := int(d.Hours() / 24)
	if n == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", n)
}
