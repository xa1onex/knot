-- Stage 6.1 — one-way folder sync jobs + per-file state
CREATE TABLE IF NOT EXISTS sync_jobs (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'one_way', -- one_way (two_way later)
  source_device_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  dest_device_id TEXT NOT NULL,
  dest_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'idle', -- idle|running|canceling|completed|failed|paused
  files_total INTEGER NOT NULL DEFAULT 0,
  files_done INTEGER NOT NULL DEFAULT 0,
  bytes_total INTEGER NOT NULL DEFAULT 0,
  bytes_done INTEGER NOT NULL DEFAULT 0,
  current_path TEXT NOT NULL DEFAULT '',
  current_transfer_id TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  last_run_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_user ON sync_jobs(user_id, created_at);

CREATE TABLE IF NOT EXISTS sync_file_state (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES sync_jobs(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL, -- path relative to source/dest roots
  size INTEGER NOT NULL DEFAULT 0,
  mtime TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  is_dir INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'synced', -- synced|pending|error
  last_synced_at TEXT,
  UNIQUE(job_id, rel_path)
);

CREATE INDEX IF NOT EXISTS idx_sync_file_state_job ON sync_file_state(job_id);
