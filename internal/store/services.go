package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	ServiceStatusRegistered = "registered"
)

type Service struct {
	ID           string
	UserID       string
	DeviceID     string
	DeviceName   string
	DeviceOnline bool
	Name         string
	Kind         string
	Protocol     string
	Port         int
	Bind         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *Store) CreateService(ctx context.Context, svc *Service) error {
	now := time.Now().UTC()
	if svc.ID == "" {
		svc.ID = NewID()
	}
	if svc.Status == "" {
		svc.Status = ServiceStatusRegistered
	}
	svc.CreatedAt = now
	svc.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO services (
  id, user_id, device_id, name, kind, protocol, port, bind, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		svc.ID, svc.UserID, svc.DeviceID, svc.Name, svc.Kind, svc.Protocol, svc.Port, svc.Bind, svc.Status,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetService(ctx context.Context, userID, id string) (*Service, error) {
	row := s.db.QueryRowContext(ctx, serviceSelect+` WHERE s.user_id = ? AND s.id = ?`, userID, id)
	return scanService(row)
}

func (s *Store) GetServiceByName(ctx context.Context, userID, deviceID, name string) (*Service, error) {
	row := s.db.QueryRowContext(ctx, serviceSelect+` WHERE s.user_id = ? AND s.device_id = ? AND s.name = ?`, userID, deviceID, name)
	return scanService(row)
}

func (s *Store) FindServiceByName(ctx context.Context, userID, name string) (*Service, error) {
	row := s.db.QueryRowContext(ctx, serviceSelect+` WHERE s.user_id = ? AND s.name = ? COLLATE NOCASE LIMIT 1`, userID, name)
	return scanService(row)
}

func (s *Store) ListServices(ctx context.Context, userID, deviceID string) ([]Service, error) {
	q := serviceSelect + ` WHERE s.user_id = ?`
	args := []any{userID}
	if deviceID != "" {
		q += ` AND s.device_id = ?`
		args = append(args, deviceID)
	}
	q += ` ORDER BY COALESCE(d.name,''), s.name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *svc)
	}
	return out, rows.Err()
}

func (s *Store) UpdateService(ctx context.Context, svc *Service) error {
	svc.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
UPDATE services SET name = ?, kind = ?, protocol = ?, port = ?, bind = ?, status = ?, updated_at = ?
WHERE user_id = ? AND id = ?`,
		svc.Name, svc.Kind, svc.Protocol, svc.Port, svc.Bind, svc.Status,
		svc.UpdatedAt.Format(time.RFC3339Nano), svc.UserID, svc.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteService(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM services WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const serviceSelect = `
SELECT s.id, s.user_id, s.device_id, COALESCE(d.name,''), COALESCE(d.online,0),
       s.name, s.kind, s.protocol, s.port, s.bind, s.status, s.created_at, s.updated_at
FROM services s
LEFT JOIN devices d ON d.id = s.device_id`

func scanService(row scanner) (*Service, error) {
	var svc Service
	var online int
	var created, updated string
	err := row.Scan(
		&svc.ID, &svc.UserID, &svc.DeviceID, &svc.DeviceName, &online,
		&svc.Name, &svc.Kind, &svc.Protocol, &svc.Port, &svc.Bind, &svc.Status, &created, &updated,
	)
	if err != nil {
		return nil, err
	}
	svc.DeviceOnline = online != 0
	svc.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	svc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &svc, nil
}
