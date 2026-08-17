package offline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	StatusPending  = "PENDING"
	StatusSyncing  = "SYNCING"
	StatusDone     = "DONE"
	StatusConflict = "CONFLICT"

	OpCreate = "create"
	OpModify = "modify"
	OpDelete = "delete"
	OpRename = "rename"

	// DefaultMaxBytes caps journal payload size (old+new state JSON + paths).
	DefaultMaxBytes int64 = 64 << 20
)

var (
	ErrDiskLimit = errors.New("offline queue disk limit exceeded")
	ErrClosed    = errors.New("offline queue closed")
)

// FileState is the snapshot stored in old_state / new_state.
type FileState struct {
	Path    string `json:"path,omitempty"`
	FileID  string `json:"file_id,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Mtime   string `json:"mtime,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Entry is one durable offline change.
type Entry struct {
	ID        string
	Operation string
	Path      string
	FileID    string
	OldState  FileState
	NewState  FileState
	Timestamp time.Time
	Status    string
	Attempts  int
	NextRetry time.Time
	Bytes     int64 // accounted payload size
}

// Config for the crash-safe queue.
type Config struct {
	// Path to SQLite file. Required.
	DBPath string
	// MaxBytes limits sum of entry payload bytes. 0 → DefaultMaxBytes.
	MaxBytes int64
}

// Queue is a crash-safe local change journal.
type Queue struct {
	db       *sql.DB
	maxBytes int64
}

func Open(cfg Config) (*Queue, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("offline queue db path required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return nil, err
	}
	max := cfg.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	db, err := sql.Open("sqlite", cfg.DBPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	q := &Queue{db: db, maxBytes: max}
	if err := q.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return q, nil
}

func (q *Queue) Close() error {
	if q == nil || q.db == nil {
		return nil
	}
	err := q.db.Close()
	q.db = nil
	return err
}

func (q *Queue) migrate() error {
	_, err := q.db.Exec(`
CREATE TABLE IF NOT EXISTS offline_entries (
  id TEXT PRIMARY KEY,
  operation TEXT NOT NULL,
  path TEXT NOT NULL,
  file_id TEXT NOT NULL DEFAULT '',
  old_state TEXT NOT NULL DEFAULT '{}',
  new_state TEXT NOT NULL DEFAULT '{}',
  ts TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_retry TEXT NOT NULL DEFAULT '',
  bytes INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_offline_status ON offline_entries(status, next_retry);
CREATE TABLE IF NOT EXISTS offline_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`)
	return err
}

func (q *Queue) MaxBytes() int64 { return q.maxBytes }

// UsageBytes is the sum of payload bytes for non-DONE rows (PENDING/SYNCING/CONFLICT).
func (q *Queue) UsageBytes(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := q.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(bytes),0) FROM offline_entries WHERE status IN (?, ?, ?)`,
		StatusPending, StatusSyncing, StatusConflict).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n.Int64, nil
}

func encodeState(s FileState) (string, int64, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", 0, err
	}
	return string(b), int64(len(b)), nil
}

// Append records a change. Rejects when disk limit would be exceeded.
func (q *Queue) Append(ctx context.Context, operation, path, fileID string, oldS, newS FileState) (*Entry, error) {
	if q == nil || q.db == nil {
		return nil, ErrClosed
	}
	oldJSON, oldN, err := encodeState(oldS)
	if err != nil {
		return nil, err
	}
	newJSON, newN, err := encodeState(newS)
	if err != nil {
		return nil, err
	}
	payload := oldN + newN + int64(len(path)+len(operation)+len(fileID))
	used, err := q.UsageBytes(ctx)
	if err != nil {
		return nil, err
	}
	if used+payload > q.maxBytes {
		return nil, ErrDiskLimit
	}
	now := time.Now().UTC()
	e := &Entry{
		ID:        uuid.NewString(),
		Operation: operation,
		Path:      path,
		FileID:    fileID,
		OldState:  oldS,
		NewState:  newS,
		Timestamp: now,
		Status:    StatusPending,
		Bytes:     payload,
	}
	_, err = q.db.ExecContext(ctx, `
INSERT INTO offline_entries (id, operation, path, file_id, old_state, new_state, ts, status, attempts, next_retry, bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '', ?)`,
		e.ID, e.Operation, e.Path, e.FileID, oldJSON, newJSON,
		e.Timestamp.Format(time.RFC3339Nano), e.Status, e.Bytes)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (q *Queue) ListByStatus(ctx context.Context, status string) ([]Entry, error) {
	rows, err := q.db.QueryContext(ctx, `
SELECT id, operation, path, file_id, old_state, new_state, ts, status, attempts, next_retry, bytes
FROM offline_entries WHERE status = ? ORDER BY ts ASC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (q *Queue) ListPending(ctx context.Context) ([]Entry, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := q.db.QueryContext(ctx, `
SELECT id, operation, path, file_id, old_state, new_state, ts, status, attempts, next_retry, bytes
FROM offline_entries
WHERE status = ? AND (next_retry = '' OR next_retry <= ?)
ORDER BY ts ASC`, StatusPending, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (q *Queue) CountPending(ctx context.Context) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM offline_entries WHERE status = ?`, StatusPending).Scan(&n)
	return n, err
}

func (q *Queue) MarkSyncing(ctx context.Context, ids []string) error {
	return q.setStatus(ctx, ids, StatusSyncing)
}

func (q *Queue) MarkDone(ctx context.Context, ids []string) error {
	return q.setStatus(ctx, ids, StatusDone)
}

func (q *Queue) MarkConflict(ctx context.Context, ids []string) error {
	return q.setStatus(ctx, ids, StatusConflict)
}

func (q *Queue) setStatus(ctx context.Context, ids []string, status string) error {
	for _, id := range ids {
		if _, err := q.db.ExecContext(ctx, `
UPDATE offline_entries SET status = ?, next_retry = '' WHERE id = ?`, status, id); err != nil {
			return err
		}
	}
	return nil
}

// MarkPendingRetry returns entries to PENDING with backoff.
func (q *Queue) MarkPendingRetry(ctx context.Context, ids []string, delay time.Duration) error {
	next := time.Now().UTC().Add(delay).Format(time.RFC3339Nano)
	for _, id := range ids {
		if _, err := q.db.ExecContext(ctx, `
UPDATE offline_entries SET status = ?, attempts = attempts + 1, next_retry = ? WHERE id = ?`,
			StatusPending, next, id); err != nil {
			return err
		}
	}
	return nil
}

// FinishFlush marks SYNCING rows DONE, or CONFLICT when path is in conflictPaths.
// conflictPaths are usually sync-root-relative (e.g. "shared.txt"); journal paths may
// include the storage prefix (e.g. "projects/shared.txt") — both forms match.
func (q *Queue) FinishFlush(ctx context.Context, conflictPaths map[string]struct{}) error {
	rows, err := q.ListByStatus(ctx, StatusSyncing)
	if err != nil {
		return err
	}
	var done, conflict []string
	for _, e := range rows {
		check := []string{e.Path}
		if e.OldState.Path != "" {
			check = append(check, e.OldState.Path)
		}
		if e.NewState.Path != "" {
			check = append(check, e.NewState.Path)
		}
		hit := false
		for _, p := range check {
			if pathInConflicts(p, conflictPaths) {
				hit = true
				break
			}
		}
		if hit {
			conflict = append(conflict, e.ID)
		} else {
			done = append(done, e.ID)
		}
	}
	if err := q.MarkDone(ctx, done); err != nil {
		return err
	}
	return q.MarkConflict(ctx, conflict)
}

func pathInConflicts(path string, conflictPaths map[string]struct{}) bool {
	if path == "" || len(conflictPaths) == 0 {
		return false
	}
	if _, ok := conflictPaths[path]; ok {
		return true
	}
	for p := range conflictPaths {
		if p == "" {
			continue
		}
		if path == p {
			return true
		}
		if len(path) > len(p) && (path[len(path)-len(p)-1] == '/' && path[len(path)-len(p):] == p) {
			return true
		}
		if len(p) > len(path) && (p[len(p)-len(path)-1] == '/' && p[len(p)-len(path):] == path) {
			return true
		}
	}
	return false
}

// CompactDone deletes DONE rows older than keep (0 = delete all DONE).
func (q *Queue) CompactDone(ctx context.Context, keep time.Duration) error {
	if keep <= 0 {
		_, err := q.db.ExecContext(ctx, `DELETE FROM offline_entries WHERE status = ?`, StatusDone)
		return err
	}
	cut := time.Now().UTC().Add(-keep).Format(time.RFC3339Nano)
	_, err := q.db.ExecContext(ctx, `DELETE FROM offline_entries WHERE status = ? AND ts < ?`, StatusDone, cut)
	return err
}

func (q *Queue) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := q.db.QueryRowContext(ctx, `SELECT value FROM offline_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (q *Queue) SetMeta(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx, `
INSERT INTO offline_meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var e Entry
		var oldJSON, newJSON, ts, nextRetry string
		if err := rows.Scan(&e.ID, &e.Operation, &e.Path, &e.FileID, &oldJSON, &newJSON, &ts, &e.Status, &e.Attempts, &nextRetry, &e.Bytes); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(oldJSON), &e.OldState)
		_ = json.Unmarshal([]byte(newJSON), &e.NewState)
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		if nextRetry != "" {
			e.NextRetry, _ = time.Parse(time.RFC3339Nano, nextRetry)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DefaultDBPath returns {dataDir}/offline-queue.db.
func DefaultDBPath(dataDir string) string {
	return filepath.Join(dataDir, "offline-queue.db")
}

// MaxBytesFromEnv reads KNOT_OFFLINE_QUEUE_MAX_BYTES or DefaultMaxBytes.
func MaxBytesFromEnv() int64 {
	v := os.Getenv("KNOT_OFFLINE_QUEUE_MAX_BYTES")
	if v == "" {
		return DefaultMaxBytes
	}
	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return DefaultMaxBytes
	}
	return n
}
