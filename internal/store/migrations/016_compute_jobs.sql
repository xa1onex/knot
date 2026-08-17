-- Stage 8.2 — one-shot compute jobs (explicit device_id, no scheduler)
CREATE TABLE IF NOT EXISTS compute_jobs (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  image TEXT NOT NULL,
  command_json TEXT NOT NULL DEFAULT '[]',
  env_json TEXT NOT NULL DEFAULT '{}',
  cpu REAL NOT NULL DEFAULT 1,
  memory_mb INTEGER NOT NULL DEFAULT 512,
  gpu INTEGER NOT NULL DEFAULT 0,
  pids INTEGER NOT NULL DEFAULT 256,
  timeout_seconds INTEGER NOT NULL DEFAULT 300,
  input_path TEXT NOT NULL DEFAULT '',
  output_path TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  exit_code INTEGER,
  error TEXT NOT NULL DEFAULT '',
  container_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_compute_jobs_user ON compute_jobs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_compute_jobs_device ON compute_jobs (device_id, status);

CREATE TABLE IF NOT EXISTS compute_job_logs (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  stream TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_compute_job_logs_job ON compute_job_logs (job_id, created_at);
