package store

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"
)

type RouteTrafficTarget struct {
	ID        string
	RouteID   string
	ReleaseID string
	Weight    int
	UpdatedAt time.Time
}

type RouteTrafficHistory struct {
	ID            string
	RouteID       string
	Action        string
	FromReleaseID string
	ToReleaseID   string
	WeightsJSON   string
	CreatedBy     string
	CreatedAt     time.Time
}

func (s *Store) UpdateEdgeRouteBinding(ctx context.Context, userID, routeID, activeReleaseID, prevReleaseID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE edge_routes SET active_release_id = ?, prev_release_id = ? WHERE id = ? AND user_id = ?`,
		activeReleaseID, prevReleaseID, routeID, userID)
	return err
}

func (s *Store) ListRouteTrafficTargets(ctx context.Context, routeID string) ([]RouteTrafficTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, route_id, release_id, weight, updated_at FROM route_traffic_targets
WHERE route_id = ? ORDER BY weight DESC, release_id`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouteTrafficTarget
	for rows.Next() {
		var t RouteTrafficTarget
		var updated string
		if err := rows.Scan(&t.ID, &t.RouteID, &t.ReleaseID, &t.Weight, &updated); err != nil {
			return nil, err
		}
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, t)
	}
	if out == nil {
		out = []RouteTrafficTarget{}
	}
	return out, rows.Err()
}

func (s *Store) ReplaceRouteTrafficTargets(ctx context.Context, routeID string, targets []RouteTrafficTarget) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM route_traffic_targets WHERE route_id = ?`, routeID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range targets {
		id := t.ID
		if id == "" {
			id = NewID()
		}
		w := t.Weight
		if w < 0 {
			w = 0
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO route_traffic_targets (id, route_id, release_id, weight, updated_at) VALUES (?, ?, ?, ?, ?)`,
			id, routeID, t.ReleaseID, w, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertRouteTrafficTarget(ctx context.Context, routeID, releaseID string, weight int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO route_traffic_targets (id, route_id, release_id, weight, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(route_id, release_id) DO UPDATE SET weight = excluded.weight, updated_at = excluded.updated_at`,
		NewID(), routeID, releaseID, weight, now)
	return err
}

func (s *Store) AppendRouteTrafficHistory(ctx context.Context, h *RouteTrafficHistory) error {
	if h.ID == "" {
		h.ID = NewID()
	}
	if h.WeightsJSON == "" {
		h.WeightsJSON = "{}"
	}
	h.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO route_traffic_history (id, route_id, action, from_release_id, to_release_id, weights_json, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.RouteID, h.Action, h.FromReleaseID, h.ToReleaseID, h.WeightsJSON, h.CreatedBy,
		h.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListRouteTrafficHistory(ctx context.Context, routeID string, limit int) ([]RouteTrafficHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, route_id, action, from_release_id, to_release_id, weights_json, created_by, created_at
FROM route_traffic_history WHERE route_id = ? ORDER BY created_at DESC LIMIT ?`, routeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouteTrafficHistory
	for rows.Next() {
		var h RouteTrafficHistory
		var created string
		if err := rows.Scan(&h.ID, &h.RouteID, &h.Action, &h.FromReleaseID, &h.ToReleaseID, &h.WeightsJSON, &h.CreatedBy, &created); err != nil {
			return nil, err
		}
		h.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, h)
	}
	if out == nil {
		out = []RouteTrafficHistory{}
	}
	return out, rows.Err()
}

func WeightsJSON(targets []RouteTrafficTarget) string {
	m := map[string]int{}
	for _, t := range targets {
		m[t.ReleaseID] = t.Weight
	}
	b, _ := json.Marshal(m)
	if len(m) == 0 {
		return "{}"
	}
	return string(b)
}

// PickTrafficReleaseID chooses a release by weight. Zero-weight targets never win.
// If all weights are 0, fallback is returned (active binding or empty).
func PickTrafficReleaseID(targets []RouteTrafficTarget, fallback string) string {
	total := 0
	for _, t := range targets {
		if t.Weight > 0 {
			total += t.Weight
		}
	}
	if total <= 0 {
		return fallback
	}
	n := rand.Intn(total)
	for _, t := range targets {
		if t.Weight <= 0 {
			continue
		}
		if n < t.Weight {
			return t.ReleaseID
		}
		n -= t.Weight
	}
	return fallback
}
