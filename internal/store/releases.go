package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	ReleaseStatusCreated    = "created"
	ReleaseStatusDeploying  = "deploying"
	ReleaseStatusTesting    = "testing"
	ReleaseStatusActive     = "active"
	ReleaseStatusFailed     = "failed"
	ReleaseStatusRolledBack = "rolled_back"
	ReleaseStatusCancelled  = "cancelled"
)

type Release struct {
	ID                   string
	UserID               string
	Number               int
	Service              string
	Image                string
	EnvironmentID        string
	EnvironmentName      string
	ConfigVersion        string
	VarsJSON             string
	SecretPinsJSON       string
	Status               string
	CreatedBy            string
	DeviceID             string
	DeviceName           string
	Port                 int
	Bind                 string
	HealthPath           string
	HealthTimeoutSeconds int
	HealthRetries        int
	HealthExpectedStatus int
	Hostname             string
	EdgeDeviceID         string
	BuildID              string
	JobID                string
	DeploymentID         string
	PrevReleaseID        string
	Current              bool
	Error                string
	TraceID              string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ReleaseLog struct {
	ID        string
	ReleaseID string
	Stream    string
	Source    string
	Message   string
	CreatedAt time.Time
}

func (s *Store) CreateRelease(ctx context.Context, r *Release) error {
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.Status == "" {
		r.Status = ReleaseStatusCreated
	}
	if r.VarsJSON == "" {
		r.VarsJSON = "{}"
	}
	if r.SecretPinsJSON == "" {
		r.SecretPinsJSON = "{}"
	}
	if r.Bind == "" {
		r.Bind = "127.0.0.1"
	}
	if r.HealthPath == "" {
		r.HealthPath = "/health"
	}
	if r.HealthTimeoutSeconds <= 0 {
		r.HealthTimeoutSeconds = 15
	}
	if r.HealthRetries <= 0 {
		r.HealthRetries = 1
	}
	if r.HealthExpectedStatus <= 0 {
		r.HealthExpectedStatus = 200
	}
	r.CreatedAt = now
	r.UpdatedAt = now
	cur := 0
	if r.Current {
		cur = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO releases (
  id, user_id, number, service, image, environment_id, environment_name, config_version,
  vars_json, secret_pins_json, status, created_by, device_id, port, bind, health_path,
  health_timeout_seconds, health_retries, health_expected_status, hostname, edge_device_id,
  build_id, job_id, deployment_id, prev_release_id, current, error, created_at, updated_at, trace_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.UserID, r.Number, r.Service, r.Image, r.EnvironmentID, r.EnvironmentName, r.ConfigVersion,
		r.VarsJSON, r.SecretPinsJSON, r.Status, r.CreatedBy, r.DeviceID, r.Port, r.Bind, r.HealthPath,
		r.HealthTimeoutSeconds, r.HealthRetries, r.HealthExpectedStatus, r.Hostname, r.EdgeDeviceID,
		r.BuildID, r.JobID, r.DeploymentID, r.PrevReleaseID, cur, r.Error,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), r.TraceID)
	return err
}

func (s *Store) UpdateRelease(ctx context.Context, r *Release) error {
	r.UpdatedAt = time.Now().UTC()
	cur := 0
	if r.Current {
		cur = 1
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE releases SET
  status = ?, device_id = ?, port = ?, bind = ?, health_path = ?, deployment_id = ?,
  current = ?, error = ?, updated_at = ?
WHERE id = ? AND user_id = ?`,
		r.Status, r.DeviceID, r.Port, r.Bind, r.HealthPath, r.DeploymentID,
		cur, r.Error, r.UpdatedAt.Format(time.RFC3339Nano), r.ID, r.UserID)
	return err
}

func (s *Store) GetRelease(ctx context.Context, userID, id string) (*Release, error) {
	row := s.db.QueryRowContext(ctx, releaseSelect+` WHERE r.id = ? AND r.user_id = ?`, id, userID)
	return scanRelease(row)
}

func (s *Store) GetReleaseByID(ctx context.Context, id string) (*Release, error) {
	row := s.db.QueryRowContext(ctx, releaseSelect+` WHERE r.id = ?`, id)
	return scanRelease(row)
}

func (s *Store) GetCurrentRelease(ctx context.Context, userID, service string) (*Release, error) {
	row := s.db.QueryRowContext(ctx, releaseSelect+` WHERE r.user_id = ? AND r.service = ? AND r.current = 1`, userID, service)
	return scanRelease(row)
}

func (s *Store) ListReleases(ctx context.Context, userID, service string) ([]Release, error) {
	q := releaseSelect + ` WHERE r.user_id = ?`
	args := []any{userID}
	if service != "" {
		q += ` AND r.service = ?`
		args = append(args, service)
	}
	q += ` ORDER BY r.service COLLATE NOCASE, r.number DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if out == nil {
		out = []Release{}
	}
	return out, rows.Err()
}

func (s *Store) MaxReleaseNumber(ctx context.Context, userID, service string) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(number), 0) FROM releases WHERE user_id = ? AND service = ?`, userID, service)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) ClearCurrentRelease(ctx context.Context, userID, service string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE releases SET current = 0, updated_at = ? WHERE user_id = ? AND service = ? AND current = 1`,
		now, userID, service)
	return err
}

func (s *Store) AppendReleaseLog(ctx context.Context, releaseID, stream, source, message string) error {
	if source == "" {
		source = "release"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO release_logs (id, release_id, stream, source, message, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		NewID(), releaseID, stream, source, message, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListReleaseLogs(ctx context.Context, releaseID string, limit int) ([]ReleaseLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, release_id, stream, source, message, created_at FROM release_logs
WHERE release_id = ? ORDER BY created_at ASC LIMIT ?`, releaseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseLog
	for rows.Next() {
		var l ReleaseLog
		var created string
		if err := rows.Scan(&l.ID, &l.ReleaseID, &l.Stream, &l.Source, &l.Message, &created); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, l)
	}
	if out == nil {
		out = []ReleaseLog{}
	}
	return out, rows.Err()
}

const releaseSelect = `
SELECT r.id, r.user_id, r.number, r.service, r.image, r.environment_id, r.environment_name,
       r.config_version, r.vars_json, r.secret_pins_json, r.status, r.created_by, r.device_id,
       COALESCE(d.name,''), r.port, r.bind, r.health_path, r.health_timeout_seconds, r.health_retries,
       r.health_expected_status, r.hostname, r.edge_device_id, r.build_id, r.job_id, r.deployment_id,
       r.prev_release_id, r.current, r.error, r.created_at, r.updated_at, COALESCE(r.trace_id,'')
FROM releases r
LEFT JOIN devices d ON d.id = r.device_id`

func scanRelease(row scanner) (*Release, error) {
	var r Release
	var current int
	var created, updated string
	if err := row.Scan(
		&r.ID, &r.UserID, &r.Number, &r.Service, &r.Image, &r.EnvironmentID, &r.EnvironmentName,
		&r.ConfigVersion, &r.VarsJSON, &r.SecretPinsJSON, &r.Status, &r.CreatedBy, &r.DeviceID,
		&r.DeviceName, &r.Port, &r.Bind, &r.HealthPath, &r.HealthTimeoutSeconds, &r.HealthRetries,
		&r.HealthExpectedStatus, &r.Hostname, &r.EdgeDeviceID, &r.BuildID, &r.JobID, &r.DeploymentID,
		&r.PrevReleaseID, &current, &r.Error, &created, &updated, &r.TraceID,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	r.Current = current != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &r, nil
}
