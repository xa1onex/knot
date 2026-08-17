-- Stage 6.2.1 — two-way sync state + conflicts
ALTER TABLE sync_file_state ADD COLUMN file_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_file_state ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_file_state ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_file_state ADD COLUMN conflict_id TEXT;

ALTER TABLE sync_jobs ADD COLUMN conflicts_open INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS sync_conflicts (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES sync_jobs(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open', -- open|resolved
  a_exists INTEGER NOT NULL DEFAULT 0,
  a_deleted INTEGER NOT NULL DEFAULT 0,
  a_size INTEGER NOT NULL DEFAULT 0,
  a_mtime TEXT NOT NULL DEFAULT '',
  a_sha256 TEXT NOT NULL DEFAULT '',
  b_exists INTEGER NOT NULL DEFAULT 0,
  b_deleted INTEGER NOT NULL DEFAULT 0,
  b_size INTEGER NOT NULL DEFAULT 0,
  b_mtime TEXT NOT NULL DEFAULT '',
  b_sha256 TEXT NOT NULL DEFAULT '',
  base_sha256 TEXT NOT NULL DEFAULT '',
  base_size INTEGER NOT NULL DEFAULT 0,
  resolution TEXT NOT NULL DEFAULT '', -- keep_a|keep_b|keep_both
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  UNIQUE(job_id, rel_path)
);

CREATE INDEX IF NOT EXISTS idx_sync_conflicts_job ON sync_conflicts(job_id, status);
