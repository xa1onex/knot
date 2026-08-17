package workflows

import (
	"context"
	"fmt"
	"strings"

	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

func (s *Service) stepBuildStatus(ctx context.Context, st *runState) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(st.req.BuildID) != "" && s.Builds != nil {
		b, err := s.Builds.Get(ctx, st.req.UserID, st.req.BuildID)
		if err != nil {
			return nil, err
		}
		out["build_id"] = b.ID
		out["status"] = b.Status
		out["image"] = b.Image
		out["tag"] = b.Tag
		if st.req.Image == "" {
			st.req.Image = b.Image
		}
		return out, nil
	}
	if s.Builds != nil {
		list, err := s.Builds.List(ctx, st.req.UserID, "", st.req.DeviceID)
		if err != nil {
			return nil, err
		}
		for i := range list {
			b := list[i]
			if b.Status == protocol.BuildStatusCompleted && b.Image != "" {
				out["build_id"] = b.ID
				out["status"] = b.Status
				out["image"] = b.Image
				out["tag"] = b.Tag
				if st.req.Image == "" {
					st.req.Image = b.Image
				}
				return out, nil
			}
		}
	}
	if strings.TrimSpace(st.req.Image) != "" {
		out["status"] = "using_image"
		out["image"] = st.req.Image
		return out, nil
	}
	return map[string]any{"status": "none"}, nil
}

func (s *Service) stepReleaseCreate(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Releases == nil {
		return nil, fmt.Errorf("releases unavailable")
	}
	if strings.TrimSpace(st.req.Service) == "" {
		return nil, fmt.Errorf("%w: service required", ErrValidation)
	}
	if strings.TrimSpace(st.req.Image) == "" {
		return nil, fmt.Errorf("%w: image or completed build required", ErrValidation)
	}
	rel, err := s.Releases.Create(ctx, releases.CreateRequest{
		UserID: st.req.UserID, CreatedBy: st.req.Actor, Service: st.req.Service,
		Image: st.req.Image, Environment: st.req.Environment, DeviceID: st.req.DeviceID,
		Port: st.req.Port, Hostname: st.req.Hostname, BuildID: st.req.BuildID,
	})
	if err != nil {
		return nil, err
	}
	st.created = rel
	return map[string]any{
		"release_id": rel.ID, "number": rel.Number, "status": rel.Status, "image": rel.Image,
	}, nil
}

func (s *Service) stepDeploy(ctx context.Context, st *runState) (map[string]any, error) {
	if s.Releases == nil || st.created == nil {
		return nil, fmt.Errorf("%w: release missing", ErrValidation)
	}
	rel, err := s.Releases.Deploy(ctx, releases.DeployRequest{
		UserID: st.req.UserID, ID: st.created.ID, DeviceID: st.req.DeviceID, Port: st.req.Port,
	})
	if rel != nil {
		st.created = rel
	}
	if err != nil {
		return map[string]any{"release_id": st.created.ID, "status": st.created.Status, "error": err.Error()}, err
	}
	return map[string]any{
		"release_id": rel.ID, "number": rel.Number, "status": rel.Status,
		"deployment_id": rel.DeploymentID, "health_ok": rel.Status == store.ReleaseStatusActive,
	}, nil
}

func (s *Service) stepHealthGate(ctx context.Context, st *runState) (map[string]any, error) {
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
	out := map[string]any{
		"release_id": rel.ID, "number": rel.Number, "status": rel.Status, "error": rel.Error,
		"traffic_switched": false,
	}
	if st.req.Hostname != "" && s.Traffic != nil {
		if tr, err := s.Traffic.Get(ctx, st.req.UserID, st.req.Hostname); err == nil {
			out["active_release_id"] = tr.ActiveReleaseID
			out["traffic_switched"] = tr.ActiveReleaseID == rel.ID
		}
	}
	if rel.Status == store.ReleaseStatusFailed {
		return out, fmt.Errorf("health gate failed: %s", rel.Error)
	}
	return out, nil
}
