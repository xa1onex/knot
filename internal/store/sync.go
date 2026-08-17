package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	SyncModeOneWay = "one_way"
	SyncModeTwoWay = "two_way"

	SyncIdle                   = "idle"
	SyncRunning                = "running"
	SyncCanceling              = "canceling"
	SyncCompleted              = "completed"
	SyncCompletedWithConflicts = "completed_with_conflicts"
	SyncFailed                 = "failed"
	SyncPaused                 = "paused"

	SyncFileSynced   = "synced"
	SyncFilePending  = "pending"
	SyncFileError    = "error"
	SyncFileConflict = "conflict"
	SyncFileDeleted  = "deleted"

	SyncConflictOpen     = "open"
	SyncConflictResolved = "resolved"

	SyncResolveKeepA    = "keep_a"
	SyncResolveKeepB    = "keep_b"
	SyncResolveKeepBoth = "keep_both"
)

type SyncJob struct {
	ID                string
	UserID            string
	Name              string
	Mode              string
	SourceDeviceID    string
	SourcePath        string
	DestDeviceID      string
	DestPath          string
	Status            string
	FilesTotal        int64
	FilesDone         int64
	BytesTotal        int64
	BytesDone         int64
	CurrentPath       string
	CurrentTransferID string
	LastError         string
	ConflictsOpen     int64
	LastRunAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SyncFileState is the last known successfully reconciled state for a relative path.
type SyncFileState struct {
	ID           string
	JobID        string
	RelPath      string
	FileID       string
	Size         int64
	Mtime        string
	SHA256       string
	IsDir        bool
	Deleted      bool
	CreatedAt    string // when first observed in sync
	Status       string
	ConflictID   string
	LastSyncedAt *time.Time
}

type SyncConflict struct {
	ID         string
	JobID      string
	RelPath    string
	Status     string
	AExists    bool
	ADeleted   bool
	ASize      int64
	AMtime     string
	ASHA256    string
	BExists    bool
	BDeleted   bool
	BSize      int64
	BMtime     string
	BSHA256    string
	BaseSHA256 string
	BaseSize   int64
	Resolution string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

func (s *Store) CreateSyncJob(ctx context.Context, j *SyncJob) error {
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	if j.Mode == "" {
		j.Mode = SyncModeOneWay
	}
	if j.Status == "" {
		j.Status = SyncIdle
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sync_jobs (
  id, user_id, name, mode, source_device_id, source_path, dest_device_id, dest_path,
  status, files_total, files_done, bytes_total, bytes_done, current_path, current_transfer_id,
  last_error, conflicts_open, last_run_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		j.ID, j.UserID, j.Name, j.Mode, j.SourceDeviceID, j.SourcePath, j.DestDeviceID, j.DestPath,
		j.Status, j.FilesTotal, j.FilesDone, j.BytesTotal, j.BytesDone, j.CurrentPath, nullStr(j.CurrentTransferID),
		j.LastError, j.ConflictsOpen, j.CreatedAt.Format(time.RFC3339Nano), j.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

const syncJobSelect = `
SELECT id, user_id, name, mode, source_device_id, source_path, dest_device_id, dest_path,
 status, files_total, files_done, bytes_total, bytes_done, current_path, COALESCE(current_transfer_id,''),
 COALESCE(last_error,''), COALESCE(conflicts_open,0), last_run_at, created_at, updated_at
FROM sync_jobs`

func (s *Store) GetSyncJob(ctx context.Context, userID, id string) (*SyncJob, error) {
	row := s.db.QueryRowContext(ctx, syncJobSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanSyncJob(row)
}

func (s *Store) GetSyncJobByID(ctx context.Context, id string) (*SyncJob, error) {
	row := s.db.QueryRowContext(ctx, syncJobSelect+` WHERE id = ?`, id)
	return scanSyncJob(row)
}

func (s *Store) ListSyncJobs(ctx context.Context, userID string) ([]SyncJob, error) {
	rows, err := s.db.QueryContext(ctx, syncJobSelect+` WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncJob
	for rows.Next() {
		j, err := scanSyncJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSyncJob(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_jobs WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (s *Store) UpdateSyncJobProgress(ctx context.Context, id string, filesDone, bytesDone int64, currentPath, transferID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_jobs SET files_done = ?, bytes_done = ?, current_path = ?, current_transfer_id = ?, updated_at = ?
WHERE id = ?`, filesDone, bytesDone, currentPath, nullStr(transferID), time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) UpdateSyncJobTotals(ctx context.Context, id string, filesTotal, bytesTotal int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_jobs SET files_total = ?, bytes_total = ?, updated_at = ? WHERE id = ?`,
		filesTotal, bytesTotal, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) UpdateSyncJobConflictsOpen(ctx context.Context, id string, n int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_jobs SET conflicts_open = ?, updated_at = ? WHERE id = ?`,
		n, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) UpdateSyncJobStatus(ctx context.Context, id, status, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var lastRun any
	if status == SyncRunning {
		lastRun = now
	}
	if lastRun != nil {
		_, err := s.db.ExecContext(ctx, `
UPDATE sync_jobs SET status = ?, last_error = ?, last_run_at = ?, current_transfer_id = NULL, updated_at = ?
WHERE id = ?`, status, errMsg, lastRun, now, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_jobs SET status = ?, last_error = ?, current_transfer_id = NULL, updated_at = ?
WHERE id = ?`, status, errMsg, now, id)
	return err
}

func (s *Store) UpsertSyncFileState(ctx context.Context, f *SyncFileState) error {
	now := time.Now().UTC()
	isDir := 0
	if f.IsDir {
		isDir = 1
	}
	deleted := 0
	if f.Deleted {
		deleted = 1
	}
	if f.CreatedAt == "" {
		f.CreatedAt = now.Format(time.RFC3339Nano)
	}
	var lastSynced any
	if f.LastSyncedAt != nil {
		lastSynced = f.LastSyncedAt.UTC().Format(time.RFC3339Nano)
	} else if f.Status == SyncFileSynced {
		lastSynced = now.Format(time.RFC3339Nano)
		f.LastSyncedAt = &now
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sync_file_state (
  id, job_id, rel_path, size, mtime, sha256, is_dir, status, last_synced_at,
  file_id, deleted, created_at, conflict_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id, rel_path) DO UPDATE SET
 size = excluded.size, mtime = excluded.mtime, sha256 = excluded.sha256, is_dir = excluded.is_dir,
 status = excluded.status, last_synced_at = excluded.last_synced_at,
 file_id = excluded.file_id, deleted = excluded.deleted,
 conflict_id = excluded.conflict_id,
 created_at = CASE WHEN sync_file_state.created_at = '' THEN excluded.created_at ELSE sync_file_state.created_at END`,
		f.ID, f.JobID, f.RelPath, f.Size, f.Mtime, f.SHA256, isDir, f.Status, lastSynced,
		f.FileID, deleted, f.CreatedAt, nullStr(f.ConflictID))
	return err
}

func (s *Store) GetSyncFileState(ctx context.Context, jobID, relPath string) (*SyncFileState, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, job_id, rel_path, size, mtime, sha256, is_dir, status, last_synced_at,
 COALESCE(file_id,''), COALESCE(deleted,0), COALESCE(created_at,''), COALESCE(conflict_id,'')
FROM sync_file_state WHERE job_id = ? AND rel_path = ?`, jobID, relPath)
	return scanSyncFileState(row)
}

func (s *Store) ListSyncFileStates(ctx context.Context, jobID string) ([]SyncFileState, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, rel_path, size, mtime, sha256, is_dir, status, last_synced_at,
 COALESCE(file_id,''), COALESCE(deleted,0), COALESCE(created_at,''), COALESCE(conflict_id,'')
FROM sync_file_state WHERE job_id = ? ORDER BY rel_path`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncFileState
	for rows.Next() {
		f, err := scanSyncFileState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSyncFileState(ctx context.Context, jobID, relPath string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_file_state WHERE job_id = ? AND rel_path = ?`, jobID, relPath)
	return err
}

func (s *Store) UpsertSyncConflict(ctx context.Context, c *SyncConflict) error {
	now := time.Now().UTC()
	c.CreatedAt = now
	if c.Status == "" {
		c.Status = SyncConflictOpen
	}
	ae, ad, be, bd := boolInt(c.AExists), boolInt(c.ADeleted), boolInt(c.BExists), boolInt(c.BDeleted)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sync_conflicts (
  id, job_id, rel_path, status,
  a_exists, a_deleted, a_size, a_mtime, a_sha256,
  b_exists, b_deleted, b_size, b_mtime, b_sha256,
  base_sha256, base_size, resolution, created_at, resolved_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT(job_id, rel_path) DO UPDATE SET
 status = excluded.status,
 a_exists = excluded.a_exists, a_deleted = excluded.a_deleted, a_size = excluded.a_size,
 a_mtime = excluded.a_mtime, a_sha256 = excluded.a_sha256,
 b_exists = excluded.b_exists, b_deleted = excluded.b_deleted, b_size = excluded.b_size,
 b_mtime = excluded.b_mtime, b_sha256 = excluded.b_sha256,
 base_sha256 = excluded.base_sha256, base_size = excluded.base_size,
 resolution = '', resolved_at = NULL`,
		c.ID, c.JobID, c.RelPath, c.Status,
		ae, ad, c.ASize, c.AMtime, c.ASHA256,
		be, bd, c.BSize, c.BMtime, c.BSHA256,
		c.BaseSHA256, c.BaseSize, c.Resolution, now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetSyncConflict(ctx context.Context, userID, conflictID string) (*SyncConflict, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT c.id, c.job_id, c.rel_path, c.status,
 c.a_exists, c.a_deleted, c.a_size, c.a_mtime, c.a_sha256,
 c.b_exists, c.b_deleted, c.b_size, c.b_mtime, c.b_sha256,
 c.base_sha256, c.base_size, COALESCE(c.resolution,''), c.created_at, c.resolved_at
FROM sync_conflicts c
JOIN sync_jobs j ON j.id = c.job_id
WHERE c.id = ? AND j.user_id = ?`, conflictID, userID)
	return scanSyncConflict(row)
}

func (s *Store) GetOpenSyncConflictByPath(ctx context.Context, jobID, relPath string) (*SyncConflict, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, job_id, rel_path, status,
 a_exists, a_deleted, a_size, a_mtime, a_sha256,
 b_exists, b_deleted, b_size, b_mtime, b_sha256,
 base_sha256, base_size, COALESCE(resolution,''), created_at, resolved_at
FROM sync_conflicts WHERE job_id = ? AND rel_path = ? AND status = ?`, jobID, relPath, SyncConflictOpen)
	return scanSyncConflict(row)
}

func (s *Store) ListSyncConflicts(ctx context.Context, jobID string, openOnly bool) ([]SyncConflict, error) {
	q := `
SELECT id, job_id, rel_path, status,
 a_exists, a_deleted, a_size, a_mtime, a_sha256,
 b_exists, b_deleted, b_size, b_mtime, b_sha256,
 base_sha256, base_size, COALESCE(resolution,''), created_at, resolved_at
FROM sync_conflicts WHERE job_id = ?`
	args := []any{jobID}
	if openOnly {
		q += ` AND status = ?`
		args = append(args, SyncConflictOpen)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncConflict
	for rows.Next() {
		c, err := scanSyncConflict(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenSyncConflicts(ctx context.Context, jobID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_conflicts WHERE job_id = ? AND status = ?`, jobID, SyncConflictOpen).Scan(&n)
	return n, err
}

func (s *Store) ResolveSyncConflict(ctx context.Context, id, resolution string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_conflicts SET status = ?, resolution = ?, resolved_at = ? WHERE id = ?`,
		SyncConflictResolved, resolution, now, id)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanSyncJob(row scanner) (*SyncJob, error) {
	var j SyncJob
	var lastRun, created, updated sql.NullString
	var xfer sql.NullString
	err := row.Scan(
		&j.ID, &j.UserID, &j.Name, &j.Mode, &j.SourceDeviceID, &j.SourcePath, &j.DestDeviceID, &j.DestPath,
		&j.Status, &j.FilesTotal, &j.FilesDone, &j.BytesTotal, &j.BytesDone, &j.CurrentPath, &xfer,
		&j.LastError, &j.ConflictsOpen, &lastRun, &created, &updated,
	)
	if err != nil {
		return nil, err
	}
	j.CurrentTransferID = xfer.String
	if lastRun.Valid && lastRun.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, lastRun.String)
		j.LastRunAt = &t
	}
	j.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
	j.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	return &j, nil
}

func scanSyncFileState(row scanner) (*SyncFileState, error) {
	var f SyncFileState
	var isDir, deleted int
	var last sql.NullString
	var conflictID sql.NullString
	err := row.Scan(
		&f.ID, &f.JobID, &f.RelPath, &f.Size, &f.Mtime, &f.SHA256, &isDir, &f.Status, &last,
		&f.FileID, &deleted, &f.CreatedAt, &conflictID,
	)
	if err != nil {
		return nil, err
	}
	f.IsDir = isDir != 0
	f.Deleted = deleted != 0
	f.ConflictID = conflictID.String
	if last.Valid && last.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, last.String)
		f.LastSyncedAt = &t
	}
	return &f, nil
}

func scanSyncConflict(row scanner) (*SyncConflict, error) {
	var c SyncConflict
	var ae, ad, be, bd int
	var created string
	var resolved sql.NullString
	err := row.Scan(
		&c.ID, &c.JobID, &c.RelPath, &c.Status,
		&ae, &ad, &c.ASize, &c.AMtime, &c.ASHA256,
		&be, &bd, &c.BSize, &c.BMtime, &c.BSHA256,
		&c.BaseSHA256, &c.BaseSize, &c.Resolution, &created, &resolved,
	)
	if err != nil {
		return nil, err
	}
	c.AExists, c.ADeleted = ae != 0, ad != 0
	c.BExists, c.BDeleted = be != 0, bd != 0
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if resolved.Valid && resolved.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, resolved.String)
		c.ResolvedAt = &t
	}
	return &c, nil
}

func (s *Store) ListSyncJobsForDevice(ctx context.Context, userID, deviceID string) ([]SyncJob, error) {
	rows, err := s.db.QueryContext(ctx, syncJobSelect+`
WHERE user_id = ? AND (source_device_id = ? OR dest_device_id = ?)
ORDER BY created_at DESC`, userID, deviceID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncJob
	for rows.Next() {
		j, err := scanSyncJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}
