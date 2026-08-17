package store

import (
	"context"
	"database/sql"
	"time"
)

type DeviceCompute struct {
	DeviceID     string
	SnapshotJSON string
	CollectedAt  time.Time
	UpdatedAt    time.Time
}

func (s *Store) UpsertDeviceCompute(ctx context.Context, deviceID, snapshotJSON string, collectedAt time.Time) error {
	now := time.Now().UTC()
	if collectedAt.IsZero() {
		collectedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO device_compute (device_id, snapshot_json, collected_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(device_id) DO UPDATE SET
  snapshot_json = excluded.snapshot_json,
  collected_at = excluded.collected_at,
  updated_at = excluded.updated_at`,
		deviceID, snapshotJSON, collectedAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetDeviceCompute(ctx context.Context, deviceID string) (*DeviceCompute, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT device_id, snapshot_json, collected_at, updated_at FROM device_compute WHERE device_id = ?`, deviceID)
	return scanDeviceCompute(row)
}

func (s *Store) ListDeviceCompute(ctx context.Context, userID string) (map[string]DeviceCompute, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT dc.device_id, dc.snapshot_json, dc.collected_at, dc.updated_at
FROM device_compute dc
JOIN devices d ON d.id = dc.device_id
WHERE d.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DeviceCompute{}
	for rows.Next() {
		c, err := scanDeviceCompute(rows)
		if err != nil {
			return nil, err
		}
		out[c.DeviceID] = *c
	}
	return out, rows.Err()
}

func scanDeviceCompute(row scanner) (*DeviceCompute, error) {
	var c DeviceCompute
	var collected, updated string
	if err := row.Scan(&c.DeviceID, &c.SnapshotJSON, &collected, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	c.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &c, nil
}

func (s *Store) UpsertDeviceLabels(ctx context.Context, deviceID string, labelsJSON string) error {
	if labelsJSON == "" {
		labelsJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO device_labels (device_id, labels_json, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(device_id) DO UPDATE SET labels_json = excluded.labels_json, updated_at = excluded.updated_at`,
		deviceID, labelsJSON, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetDeviceLabels(ctx context.Context, deviceID string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT labels_json FROM device_labels WHERE device_id = ?`, deviceID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "{}", nil
	}
	return raw, err
}

func (s *Store) ListDeviceLabels(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT dl.device_id, dl.labels_json
FROM device_labels dl
JOIN devices d ON d.id = dl.device_id
WHERE d.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		out[id] = raw
	}
	return out, rows.Err()
}
