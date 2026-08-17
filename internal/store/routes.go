package store

import (
	"context"
	"database/sql"
	"time"
)

type EdgeRoute struct {
	ID              string
	UserID          string
	Hostname        string
	ServiceID       string
	EdgeDeviceID    string
	TLSMode         string
	EdgeName        string
	ServiceName     string
	DeviceID        string
	DeviceName      string
	Bind            string
	Port            int
	Protocol        string
	Kind            string
	ActiveReleaseID string
	PrevReleaseID   string
	CreatedAt       time.Time
}

func (s *Store) CreateEdgeRoute(ctx context.Context, r *EdgeRoute) error {
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.TLSMode == "" {
		r.TLSMode = "edge_terminate"
	}
	r.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO edge_routes (id, user_id, hostname, service_id, edge_device_id, tls_mode, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.UserID, r.Hostname, r.ServiceID, r.EdgeDeviceID, r.TLSMode, now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetEdgeRoute(ctx context.Context, userID, id string) (*EdgeRoute, error) {
	row := s.db.QueryRowContext(ctx, edgeRouteSelect+` WHERE r.user_id = ? AND r.id = ?`, userID, id)
	return scanEdgeRoute(row)
}

func (s *Store) GetEdgeRouteByHost(ctx context.Context, hostname string) (*EdgeRoute, error) {
	row := s.db.QueryRowContext(ctx, edgeRouteSelect+` WHERE r.hostname = ?`, hostname)
	return scanEdgeRoute(row)
}

func (s *Store) ListEdgeRoutes(ctx context.Context, userID string) ([]EdgeRoute, error) {
	rows, err := s.db.QueryContext(ctx, edgeRouteSelect+` WHERE r.user_id = ? ORDER BY r.hostname`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EdgeRoute
	for rows.Next() {
		rt, err := scanEdgeRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rt)
	}
	return out, rows.Err()
}

func (s *Store) ListEdgeRoutesByService(ctx context.Context, userID, serviceID string) ([]EdgeRoute, error) {
	rows, err := s.db.QueryContext(ctx, edgeRouteSelect+` WHERE r.user_id = ? AND r.service_id = ? ORDER BY r.hostname`, userID, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EdgeRoute
	for rows.Next() {
		rt, err := scanEdgeRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rt)
	}
	return out, rows.Err()
}

func (s *Store) DeleteEdgeRoute(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM edge_routes WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const edgeRouteSelect = `
SELECT r.id, r.user_id, r.hostname, r.service_id, r.edge_device_id, COALESCE(r.tls_mode,'edge_terminate'), COALESCE(e.name,''),
       s.name, s.device_id, COALESCE(d.name,''), s.bind, s.port, s.protocol, s.kind, r.created_at,
       COALESCE(r.active_release_id,''), COALESCE(r.prev_release_id,'')
FROM edge_routes r
JOIN services s ON s.id = r.service_id
LEFT JOIN devices d ON d.id = s.device_id
LEFT JOIN devices e ON e.id = r.edge_device_id`

func scanEdgeRoute(row scanner) (*EdgeRoute, error) {
	var r EdgeRoute
	var created string
	err := row.Scan(
		&r.ID, &r.UserID, &r.Hostname, &r.ServiceID, &r.EdgeDeviceID, &r.TLSMode, &r.EdgeName,
		&r.ServiceName, &r.DeviceID, &r.DeviceName, &r.Bind, &r.Port, &r.Protocol, &r.Kind, &created,
		&r.ActiveReleaseID, &r.PrevReleaseID,
	)
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &r, nil
}
