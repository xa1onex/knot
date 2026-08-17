-- Stage 8.4 — job artifacts as Storage objects linked to a job
CREATE TABLE IF NOT EXISTS compute_job_artifacts (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  file_id TEXT NOT NULL,
  path TEXT NOT NULL,
  name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  mime_type TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_compute_job_artifacts_job ON compute_job_artifacts (job_id);
CREATE INDEX IF NOT EXISTS idx_compute_job_artifacts_file ON compute_job_artifacts (file_id);
