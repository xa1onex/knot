package store

import (
	"context"
	"database/sql"
	"time"
)

type AppSource struct {
	ID                 string
	UserID             string
	Type               string
	Name               string
	URL                string
	Branch             string
	GitTag             string
	Revision           string
	CredentialSecretID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Build struct {
	ID               string
	UserID           string
	SourceID         string
	DeviceID         string
	DeviceName       string
	DeviceOnline     bool
	Dockerfile       string
	Context          string
	Tag              string
	Image            string
	Status           string
	Error            string
	GitRevision      string
	RegistrySecretID string
	TimeoutSeconds   int
	TraceID          string
	CreatedAt        time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

type BuildLog struct {
	ID        string
	BuildID   string
	Stream    string
	Message   string
	CreatedAt time.Time
}

func (s *Store) CreateAppSource(ctx context.Context, src *AppSource) error {
	now := time.Now().UTC()
	if src.ID == "" {
		src.ID = NewID()
	}
	if src.Type == "" {
		src.Type = "git"
	}
	src.CreatedAt = now
	src.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO app_sources (
  id, user_id, type, name, url, branch, git_tag, revision, credential_secret_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.UserID, src.Type, src.Name, src.URL, src.Branch, src.GitTag, src.Revision,
		src.CredentialSecretID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetAppSource(ctx context.Context, userID, id string) (*AppSource, error) {
	row := s.db.QueryRowContext(ctx, appSourceSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanAppSource(row)
}

func (s *Store) ListAppSources(ctx context.Context, userID string) ([]AppSource, error) {
	rows, err := s.db.QueryContext(ctx, appSourceSelect+` WHERE user_id = ? ORDER BY created_at DESC LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSource
	for rows.Next() {
		src, err := scanAppSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *src)
	}
	if out == nil {
		out = []AppSource{}
	}
	return out, rows.Err()
}

func (s *Store) UpdateAppSourceRevision(ctx context.Context, userID, id, revision string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE app_sources SET revision = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		revision, now, id, userID)
	return err
}

func (s *Store) CreateBuild(ctx context.Context, b *Build) error {
	now := time.Now().UTC()
	if b.ID == "" {
		b.ID = NewID()
	}
	if b.Status == "" {
		b.Status = "queued"
	}
	if b.TimeoutSeconds <= 0 {
		b.TimeoutSeconds = 600
	}
	b.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO builds (
  id, user_id, source_id, device_id, dockerfile, context, tag, image, status, error, git_revision,
  registry_secret_id, timeout_seconds, created_at, started_at, finished_at, trace_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.UserID, b.SourceID, b.DeviceID, b.Dockerfile, b.Context, b.Tag, b.Image, b.Status, b.Error,
		b.GitRevision, b.RegistrySecretID, b.TimeoutSeconds, now.Format(time.RFC3339Nano),
		nilTime(b.StartedAt), nilTime(b.FinishedAt), b.TraceID)
	return err
}

func (s *Store) UpdateBuild(ctx context.Context, b *Build) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE builds SET
  status = ?, error = ?, image = ?, git_revision = ?, started_at = ?, finished_at = ?
WHERE id = ? AND user_id = ?`,
		b.Status, b.Error, b.Image, b.GitRevision, nilTime(b.StartedAt), nilTime(b.FinishedAt), b.ID, b.UserID)
	return err
}

func (s *Store) GetBuild(ctx context.Context, userID, id string) (*Build, error) {
	row := s.db.QueryRowContext(ctx, buildSelect+` WHERE b.id = ? AND b.user_id = ?`, id, userID)
	return scanBuild(row)
}

func (s *Store) GetBuildByID(ctx context.Context, id string) (*Build, error) {
	row := s.db.QueryRowContext(ctx, buildSelect+` WHERE b.id = ?`, id)
	return scanBuild(row)
}

func (s *Store) ListBuilds(ctx context.Context, userID, sourceID, deviceID string) ([]Build, error) {
	q := buildSelect + ` WHERE b.user_id = ?`
	args := []any{userID}
	if sourceID != "" {
		q += ` AND b.source_id = ?`
		args = append(args, sourceID)
	}
	if deviceID != "" {
		q += ` AND b.device_id = ?`
		args = append(args, deviceID)
	}
	q += ` ORDER BY b.created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	if out == nil {
		out = []Build{}
	}
	return out, rows.Err()
}

func (s *Store) ListInflightBuildsByDevice(ctx context.Context, deviceID string) ([]Build, error) {
	rows, err := s.db.QueryContext(ctx, buildSelect+`
WHERE b.device_id = ? AND b.status IN ('queued','cloning','building','pushing')`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) AppendBuildLog(ctx context.Context, buildID, stream, message string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO build_logs (id, build_id, stream, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		NewID(), buildID, stream, message, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListBuildLogs(ctx context.Context, buildID string, limit int) ([]BuildLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, build_id, stream, message, created_at FROM build_logs
WHERE build_id = ? ORDER BY created_at ASC LIMIT ?`, buildID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BuildLog
	for rows.Next() {
		var l BuildLog
		var created string
		if err := rows.Scan(&l.ID, &l.BuildID, &l.Stream, &l.Message, &created); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, l)
	}
	if out == nil {
		out = []BuildLog{}
	}
	return out, rows.Err()
}

const appSourceSelect = `
SELECT id, user_id, type, name, url, branch, git_tag, revision, credential_secret_id, created_at, updated_at
FROM app_sources`

func scanAppSource(row scanner) (*AppSource, error) {
	var src AppSource
	var created, updated string
	if err := row.Scan(
		&src.ID, &src.UserID, &src.Type, &src.Name, &src.URL, &src.Branch, &src.GitTag, &src.Revision,
		&src.CredentialSecretID, &created, &updated,
	); err != nil {
		return nil, err
	}
	src.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	src.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &src, nil
}

const buildSelect = `
SELECT b.id, b.user_id, b.source_id, b.device_id, COALESCE(d.name,''), COALESCE(d.online,0),
       b.dockerfile, b.context, b.tag, b.image, b.status, b.error, b.git_revision,
       b.registry_secret_id, b.timeout_seconds, b.created_at, b.started_at, b.finished_at,
       COALESCE(b.trace_id,'')
FROM builds b
LEFT JOIN devices d ON d.id = b.device_id`

func scanBuild(row scanner) (*Build, error) {
	var b Build
	var online int
	var created string
	var started, finished sql.NullString
	if err := row.Scan(
		&b.ID, &b.UserID, &b.SourceID, &b.DeviceID, &b.DeviceName, &online,
		&b.Dockerfile, &b.Context, &b.Tag, &b.Image, &b.Status, &b.Error, &b.GitRevision,
		&b.RegistrySecretID, &b.TimeoutSeconds, &created, &started, &finished, &b.TraceID,
	); err != nil {
		return nil, err
	}
	b.DeviceOnline = online != 0
	b.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if started.Valid {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		b.StartedAt = &t
	}
	if finished.Valid {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		b.FinishedAt = &t
	}
	return &b, nil
}
