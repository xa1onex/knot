package edge

import (
	"context"

	"github.com/knot-infra/knot/internal/store"
)

// Origin is the loopback backend the Edge tunnel should dial for a request.
type Origin struct {
	DeviceID  string
	Port      int
	ServiceID string
	ReleaseID string
}

// ResolveOrigin picks the live backend for a route.
// If traffic weights are set, a weighted release wins; otherwise active_release_id;
// otherwise the registered service (pre-9.4 routes).
func (p *Proxy) ResolveOrigin(rt *store.EdgeRoute) Origin {
	if rt == nil {
		return Origin{}
	}
	out := Origin{DeviceID: rt.DeviceID, Port: rt.Port, ServiceID: rt.ServiceID, ReleaseID: rt.ActiveReleaseID}
	if p == nil || p.Store == nil {
		return out
	}
	ctx := context.Background()
	targets, err := p.Store.ListRouteTrafficTargets(ctx, rt.ID)
	if err != nil {
		return out
	}
	pick := store.PickTrafficReleaseID(targets, rt.ActiveReleaseID)
	if pick == "" {
		return out
	}
	rel, err := p.Store.GetRelease(ctx, rt.UserID, pick)
	if err != nil || rel == nil || rel.DeviceID == "" || rel.Port < 1 {
		return out
	}
	out.DeviceID = rel.DeviceID
	out.Port = rel.Port
	out.ReleaseID = rel.ID
	return out
}
