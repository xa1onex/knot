package store

import (
	"context"
	"database/sql"
	"time"
)

type ComputeJob struct {
	ID             string
	UserID         string
	DeviceID       string
	DeviceName     string
	DeviceOnline   bool
	Image          string
	CommandJSON    string
	EnvJSON        string
	CPU            float64
	MemoryMB       int64
	GPU            int
	Pids           int64
	DiskMB         int64
	TimeoutSeconds int
	InputPath      string
	OutputPath     string
	Status         string
	Reason         string
	ExitCode       *int
	Error          string
	ContainerID    string
	Placement      string
	RequireLabels  string
	PreferLabels   string
	Attempts       int
	MaxRetries     int
	SourcePath     string
	TraceID        string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

type ComputeJobLog struct {
	ID        string
	JobID     string
	Stream    string
	Message   string
	CreatedAt time.Time
}

func (s *Store) CreateComputeJob(ctx context.Context, j *ComputeJob) error {
	now := time.Now().UTC()
	if j.ID == "" {
		j.ID = NewID()
	}
	if j.Status == "" {
		j.Status = "queued"
	}
	if j.Placement == "" {
		j.Placement = "pinned"
	}
	if j.RequireLabels == "" {
		j.RequireLabels = "{}"
	}
	if j.PreferLabels == "" {
		j.PreferLabels = "{}"
	}
	j.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO compute_jobs (
  id, user_id, device_id, image, command_json, env_json, cpu, memory_mb, gpu, pids, disk_mb,
  timeout_seconds, input_path, output_path, status, reason, exit_code, error, container_id,
  created_at, started_at, finished_at,
  placement, require_labels, prefer_labels, attempts, max_retries, source_path, trace_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.UserID, j.DeviceID, j.Image, j.CommandJSON, j.EnvJSON, j.CPU, j.MemoryMB, j.GPU, j.Pids, j.DiskMB,
		j.TimeoutSeconds, j.InputPath, j.OutputPath, j.Status, j.Reason, j.ExitCode, j.Error, j.ContainerID,
		now.Format(time.RFC3339Nano), nilTime(j.StartedAt), nilTime(j.FinishedAt),
		j.Placement, j.RequireLabels, j.PreferLabels, j.Attempts, j.MaxRetries, j.SourcePath, j.TraceID)
	return err
}

func (s *Store) UpdateComputeJob(ctx context.Context, j *ComputeJob) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE compute_jobs SET
  device_id = ?, status = ?, reason = ?, exit_code = ?, error = ?, container_id = ?, output_path = ?,
  started_at = ?, finished_at = ?, attempts = ?, placement = ?
WHERE id = ? AND user_id = ?`,
		j.DeviceID, j.Status, j.Reason, j.ExitCode, j.Error, j.ContainerID, j.OutputPath,
		nilTime(j.StartedAt), nilTime(j.FinishedAt), j.Attempts, j.Placement, j.ID, j.UserID)
	return err
}

func (s *Store) GetComputeJob(ctx context.Context, userID, id string) (*ComputeJob, error) {
	row := s.db.QueryRowContext(ctx, computeJobSelect+` WHERE j.id = ? AND j.user_id = ?`, id, userID)
	return scanComputeJob(row)
}

func (s *Store) GetComputeJobByID(ctx context.Context, id string) (*ComputeJob, error) {
	row := s.db.QueryRowContext(ctx, computeJobSelect+` WHERE j.id = ?`, id)
	return scanComputeJob(row)
}

func (s *Store) ListComputeJobs(ctx context.Context, userID, deviceID string) ([]ComputeJob, error) {
	q := computeJobSelect + ` WHERE j.user_id = ?`
	args := []any{userID}
	if deviceID != "" {
		q += ` AND j.device_id = ?`
		args = append(args, deviceID)
	}
	q += ` ORDER BY j.created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJob
	for rows.Next() {
		j, err := scanComputeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	if out == nil {
		out = []ComputeJob{}
	}
	return out, rows.Err()
}

func (s *Store) ListRunningComputeJobsByDevice(ctx context.Context, deviceID string) ([]ComputeJob, error) {
	rows, err := s.db.QueryContext(ctx, computeJobSelect+`
WHERE j.device_id = ? AND j.status IN ('queued','assigned','running')`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJob
	for rows.Next() {
		j, err := scanComputeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) ListPendingScheduleJobs(ctx context.Context, userID string) ([]ComputeJob, error) {
	rows, err := s.db.QueryContext(ctx, computeJobSelect+`
WHERE j.user_id = ? AND j.placement = 'scheduled' AND j.status IN ('queued','waiting_for_resource')
ORDER BY j.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJob
	for rows.Next() {
		j, err := scanComputeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) ListInflightComputeJobs(ctx context.Context, userID string) ([]ComputeJob, error) {
	rows, err := s.db.QueryContext(ctx, computeJobSelect+`
WHERE j.user_id = ? AND j.status IN ('assigned','running')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJob
	for rows.Next() {
		j, err := scanComputeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) AppendComputeJobLog(ctx context.Context, jobID, stream, message string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO compute_job_logs (id, job_id, stream, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		NewID(), jobID, stream, message, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListComputeJobLogs(ctx context.Context, jobID string, limit int) ([]ComputeJobLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, stream, message, created_at FROM compute_job_logs
WHERE job_id = ? ORDER BY created_at ASC LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJobLog
	for rows.Next() {
		var l ComputeJobLog
		var created string
		if err := rows.Scan(&l.ID, &l.JobID, &l.Stream, &l.Message, &created); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, l)
	}
	if out == nil {
		out = []ComputeJobLog{}
	}
	return out, rows.Err()
}

type ComputeJobArtifact struct {
	ID        string
	JobID     string
	FileID    string
	Path      string
	Name      string
	Size      int64
	SHA256    string
	MimeType  string
	CreatedAt time.Time
}

func (s *Store) InsertComputeJobArtifact(ctx context.Context, a *ComputeJobArtifact) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO compute_job_artifacts (id, job_id, file_id, path, name, size, sha256, mime_type, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.JobID, a.FileID, a.Path, a.Name, a.Size, a.SHA256, a.MimeType,
		a.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListComputeJobArtifacts(ctx context.Context, jobID string) ([]ComputeJobArtifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, file_id, path, name, size, sha256, mime_type, created_at
FROM compute_job_artifacts WHERE job_id = ? ORDER BY path ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComputeJobArtifact
	for rows.Next() {
		var a ComputeJobArtifact
		var created string
		if err := rows.Scan(&a.ID, &a.JobID, &a.FileID, &a.Path, &a.Name, &a.Size, &a.SHA256, &a.MimeType, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, a)
	}
	if out == nil {
		out = []ComputeJobArtifact{}
	}
	return out, rows.Err()
}

func (s *Store) DeleteComputeJobArtifacts(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM compute_job_artifacts WHERE job_id = ?`, jobID)
	return err
}

func nilTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

const computeJobSelect = `
SELECT j.id, j.user_id, j.device_id, COALESCE(d.name,''), COALESCE(d.online,0),
       j.image, j.command_json, j.env_json, j.cpu, j.memory_mb, j.gpu, j.pids, j.disk_mb,
       j.timeout_seconds, j.input_path, j.output_path, j.status, j.reason, j.exit_code, j.error, j.container_id,
       j.created_at, j.started_at, j.finished_at,
       COALESCE(j.placement,'pinned'), COALESCE(j.require_labels,'{}'), COALESCE(j.prefer_labels,'{}'),
       COALESCE(j.attempts,0), COALESCE(j.max_retries,0), COALESCE(j.source_path,''), COALESCE(j.trace_id,'')
FROM compute_jobs j
LEFT JOIN devices d ON d.id = j.device_id`

func scanComputeJob(row scanner) (*ComputeJob, error) {
	var j ComputeJob
	var online int
	var exit sql.NullInt64
	var created string
	var started, finished sql.NullString
	if err := row.Scan(
		&j.ID, &j.UserID, &j.DeviceID, &j.DeviceName, &online,
		&j.Image, &j.CommandJSON, &j.EnvJSON, &j.CPU, &j.MemoryMB, &j.GPU, &j.Pids, &j.DiskMB,
		&j.TimeoutSeconds, &j.InputPath, &j.OutputPath, &j.Status, &j.Reason, &exit, &j.Error, &j.ContainerID,
		&created, &started, &finished,
		&j.Placement, &j.RequireLabels, &j.PreferLabels, &j.Attempts, &j.MaxRetries, &j.SourcePath, &j.TraceID,
	); err != nil {
		return nil, err
	}
	j.DeviceOnline = online != 0
	if exit.Valid {
		v := int(exit.Int64)
		j.ExitCode = &v
	}
	j.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if started.Valid && started.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		j.StartedAt = &t
	}
	if finished.Valid && finished.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		j.FinishedAt = &t
	}
	return &j, nil
}
