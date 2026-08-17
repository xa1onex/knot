package store

import (
	"context"
	"database/sql"
	"time"
)

type OpsLog struct {
	ID           string
	UserID       string
	CreatedAt    time.Time
	Level        string
	Source       string
	Message      string
	TraceID      string
	DeviceID     string
	ServiceID    string
	ServiceName  string
	ReleaseID    string
	BuildID      string
	JobID        string
	DeploymentID string
	MetadataJSON string
}

type OpsLogQuery struct {
	UserID       string
	Service      string
	ServiceID    string
	ReleaseID    string
	BuildID      string
	JobID        string
	DeploymentID string
	Source       string
	TraceID      string
	Level        string
	Q            string
	AfterID      string
	Since        *time.Time
	Until        *time.Time
	Limit        int
}

func (s *Store) InsertOpsLog(ctx context.Context, e *OpsLog) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Level == "" {
		e.Level = "info"
	}
	if e.MetadataJSON == "" {
		e.MetadataJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO ops_logs (
  id, user_id, created_at, level, source, message, trace_id, device_id, service_id, service_name,
  release_id, build_id, job_id, deployment_id, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.UserID, e.CreatedAt.UTC().Format(time.RFC3339Nano), e.Level, e.Source, e.Message,
		e.TraceID, e.DeviceID, e.ServiceID, e.ServiceName, e.ReleaseID, e.BuildID, e.JobID,
		e.DeploymentID, e.MetadataJSON)
	return err
}

func (s *Store) GetOpsLog(ctx context.Context, userID, id string) (*OpsLog, error) {
	row := s.db.QueryRowContext(ctx, opsLogSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanOpsLog(row)
}

func (s *Store) ListOpsLogs(ctx context.Context, q OpsLogQuery) ([]OpsLog, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	sqlStr := opsLogSelect + ` WHERE user_id = ?`
	args := []any{q.UserID}
	if q.Service != "" {
		sqlStr += ` AND service_name = ?`
		args = append(args, q.Service)
	}
	if q.ServiceID != "" {
		sqlStr += ` AND service_id = ?`
		args = append(args, q.ServiceID)
	}
	if q.ReleaseID != "" {
		sqlStr += ` AND release_id = ?`
		args = append(args, q.ReleaseID)
	}
	if q.BuildID != "" {
		sqlStr += ` AND build_id = ?`
		args = append(args, q.BuildID)
	}
	if q.JobID != "" {
		sqlStr += ` AND job_id = ?`
		args = append(args, q.JobID)
	}
	if q.DeploymentID != "" {
		sqlStr += ` AND deployment_id = ?`
		args = append(args, q.DeploymentID)
	}
	if q.Source != "" {
		sqlStr += ` AND source = ?`
		args = append(args, q.Source)
	}
	if q.TraceID != "" {
		sqlStr += ` AND trace_id = ?`
		args = append(args, q.TraceID)
	}
	if q.Level != "" {
		sqlStr += ` AND level = ?`
		args = append(args, q.Level)
	}
	if q.Q != "" {
		sqlStr += ` AND message LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(q.Q)+"%")
	}
	if q.Since != nil {
		sqlStr += ` AND created_at >= ?`
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	if q.Until != nil {
		sqlStr += ` AND created_at <= ?`
		args = append(args, q.Until.UTC().Format(time.RFC3339Nano))
	}

	afterSet := false
	if q.AfterID != "" {
		var ts string
		err := s.db.QueryRowContext(ctx, `SELECT created_at FROM ops_logs WHERE id = ? AND user_id = ?`, q.AfterID, q.UserID).Scan(&ts)
		if err == nil {
			sqlStr += ` AND (created_at > ? OR (created_at = ? AND id > ?))`
			args = append(args, ts, ts, q.AfterID)
			afterSet = true
		}
	}
	if afterSet {
		sqlStr += ` ORDER BY created_at ASC, id ASC LIMIT ?`
		args = append(args, limit)
	} else {
		sqlStr += ` ORDER BY created_at DESC, id DESC LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpsLog
	for rows.Next() {
		e, err := scanOpsLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !afterSet {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if out == nil {
		out = []OpsLog{}
	}
	return out, nil
}

func (s *Store) DeleteOpsLogsBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ops_logs WHERE created_at < ?`, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const opsLogSelect = `
SELECT id, user_id, created_at, level, source, message, trace_id, device_id, service_id, service_name,
       release_id, build_id, job_id, deployment_id, metadata_json
FROM ops_logs`

func scanOpsLog(row scanner) (*OpsLog, error) {
	var e OpsLog
	var created string
	if err := row.Scan(
		&e.ID, &e.UserID, &created, &e.Level, &e.Source, &e.Message, &e.TraceID, &e.DeviceID,
		&e.ServiceID, &e.ServiceName, &e.ReleaseID, &e.BuildID, &e.JobID, &e.DeploymentID, &e.MetadataJSON,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &e, nil
}
