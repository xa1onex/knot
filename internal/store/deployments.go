package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	DeployStatusPending   = "pending"
	DeployStatusStarting  = "starting"
	DeployStatusRunning   = "running"
	DeployStatusStopped   = "stopped"
	DeployStatusFailed    = "failed"
	DeployStatusUnhealthy = "unhealthy"
)

type Deployment struct {
	ID               string
	UserID           string
	DeviceID         string
	DeviceName       string
	DeviceOnline     bool
	ServiceID        string
	Name             string
	Runtime          string
	Image            string
	Port             int
	Bind             string
	EnvJSON          string
	HealthPath       string
	Revision         int
	Status           string
	ContainerID      string
	PrevDeploymentID string
	Active           bool
	HealthOK         bool
	Error            string
	EnvironmentID    string
	SecretPinsJSON   string
	ReleaseID        string
	TraceID          string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DeploymentLog struct {
	ID           string
	DeploymentID string
	Stream       string
	Message      string
	CreatedAt    time.Time
}

func (s *Store) CreateDeployment(ctx context.Context, d *Deployment) error {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = NewID()
	}
	if d.Runtime == "" {
		d.Runtime = "docker"
	}
	if d.Bind == "" {
		d.Bind = "127.0.0.1"
	}
	if d.HealthPath == "" {
		d.HealthPath = "/"
	}
	if d.Status == "" {
		d.Status = DeployStatusPending
	}
	if d.SecretPinsJSON == "" {
		d.SecretPinsJSON = "{}"
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	active := 0
	if d.Active {
		active = 1
	}
	health := 0
	if d.HealthOK {
		health = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO deployments (
  id, user_id, device_id, service_id, name, runtime, image, port, bind, env_json, health_path,
  revision, status, container_id, prev_deployment_id, active, health_ok, error, created_at, updated_at,
  environment_id, secret_pins_json, release_id, trace_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.DeviceID, d.ServiceID, d.Name, d.Runtime, d.Image, d.Port, d.Bind, d.EnvJSON, d.HealthPath,
		d.Revision, d.Status, d.ContainerID, d.PrevDeploymentID, active, health, d.Error,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), d.EnvironmentID, d.SecretPinsJSON, d.ReleaseID, d.TraceID)
	return err
}

func (s *Store) GetDeployment(ctx context.Context, userID, id string) (*Deployment, error) {
	row := s.db.QueryRowContext(ctx, deploymentSelect+` WHERE d.user_id = ? AND d.id = ?`, userID, id)
	return scanDeployment(row)
}

func (s *Store) GetDeploymentByID(ctx context.Context, id string) (*Deployment, error) {
	row := s.db.QueryRowContext(ctx, deploymentSelect+` WHERE d.id = ?`, id)
	return scanDeployment(row)
}

func (s *Store) GetActiveDeploymentByName(ctx context.Context, userID, deviceID, name string) (*Deployment, error) {
	row := s.db.QueryRowContext(ctx, deploymentSelect+` WHERE d.user_id = ? AND d.device_id = ? AND d.name = ? AND d.active = 1`, userID, deviceID, name)
	return scanDeployment(row)
}

func (s *Store) ListActiveDeploymentsByDevice(ctx context.Context, deviceID string) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, deploymentSelect+` WHERE d.device_id = ? AND d.active = 1`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		dep, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dep)
	}
	return out, rows.Err()
}

func (s *Store) ListDeployments(ctx context.Context, userID, deviceID, name string) ([]Deployment, error) {
	q := deploymentSelect + ` WHERE d.user_id = ?`
	args := []any{userID}
	if deviceID != "" {
		q += ` AND d.device_id = ?`
		args = append(args, deviceID)
	}
	if name != "" {
		q += ` AND d.name = ?`
		args = append(args, name)
	}
	q += ` ORDER BY d.name COLLATE NOCASE, d.revision DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		dep, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dep)
	}
	return out, rows.Err()
}

func (s *Store) MaxDeploymentRevision(ctx context.Context, userID, deviceID, name string) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision), 0) FROM deployments WHERE user_id = ? AND device_id = ? AND name = ?`,
		userID, deviceID, name)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) DeactivateDeployments(ctx context.Context, userID, deviceID, name string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE deployments SET active = 0, updated_at = ? WHERE user_id = ? AND device_id = ? AND name = ? AND active = 1`,
		now, userID, deviceID, name)
	return err
}

func (s *Store) UpdateDeployment(ctx context.Context, d *Deployment) error {
	d.UpdatedAt = time.Now().UTC()
	active := 0
	if d.Active {
		active = 1
	}
	health := 0
	if d.HealthOK {
		health = 1
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE deployments SET service_id = ?, image = ?, port = ?, bind = ?, env_json = ?, health_path = ?,
  revision = ?, status = ?, container_id = ?, prev_deployment_id = ?, active = ?, health_ok = ?, error = ?,
  environment_id = ?, secret_pins_json = ?, release_id = ?, updated_at = ?
WHERE user_id = ? AND id = ?`,
		d.ServiceID, d.Image, d.Port, d.Bind, d.EnvJSON, d.HealthPath,
		d.Revision, d.Status, d.ContainerID, d.PrevDeploymentID, active, health, d.Error,
		d.EnvironmentID, d.SecretPinsJSON, d.ReleaseID, d.UpdatedAt.Format(time.RFC3339Nano), d.UserID, d.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AppendDeploymentLog(ctx context.Context, deploymentID, stream, message string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO deployment_logs (id, deployment_id, stream, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		NewID(), deploymentID, stream, message, now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListDeploymentLogs(ctx context.Context, deploymentID string, limit int) ([]DeploymentLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, deployment_id, stream, message, created_at FROM deployment_logs
WHERE deployment_id = ? ORDER BY created_at DESC LIMIT ?`, deploymentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeploymentLog
	for rows.Next() {
		var l DeploymentLog
		var created string
		if err := rows.Scan(&l.ID, &l.DeploymentID, &l.Stream, &l.Message, &created); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, l)
	}
	return out, rows.Err()
}

const deploymentSelect = `
SELECT d.id, d.user_id, d.device_id, COALESCE(dev.name,''), COALESCE(dev.online,0),
       d.service_id, d.name, d.runtime, d.image, d.port, d.bind, d.env_json, d.health_path,
       d.revision, d.status, d.container_id, d.prev_deployment_id, d.active, d.health_ok, d.error,
       d.created_at, d.updated_at, COALESCE(d.environment_id,''), COALESCE(d.secret_pins_json,'{}'), COALESCE(d.release_id,''),
       COALESCE(d.trace_id,'')
FROM deployments d
LEFT JOIN devices dev ON dev.id = d.device_id`

func scanDeployment(row scanner) (*Deployment, error) {
	var d Deployment
	var online, active, health int
	var created, updated string
	err := row.Scan(
		&d.ID, &d.UserID, &d.DeviceID, &d.DeviceName, &online,
		&d.ServiceID, &d.Name, &d.Runtime, &d.Image, &d.Port, &d.Bind, &d.EnvJSON, &d.HealthPath,
		&d.Revision, &d.Status, &d.ContainerID, &d.PrevDeploymentID, &active, &health, &d.Error,
		&created, &updated, &d.EnvironmentID, &d.SecretPinsJSON, &d.ReleaseID, &d.TraceID,
	)
	if err != nil {
		return nil, err
	}
	d.DeviceOnline = online != 0
	d.Active = active != 0
	d.HealthOK = health != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &d, nil
}
