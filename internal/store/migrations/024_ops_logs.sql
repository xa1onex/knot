-- Stage 9.5 — unified operational logs (not an ELK clone)
CREATE TABLE IF NOT EXISTS ops_logs (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT 'info',
  source TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  trace_id TEXT NOT NULL DEFAULT '',
  device_id TEXT NOT NULL DEFAULT '',
  service_id TEXT NOT NULL DEFAULT '',
  service_name TEXT NOT NULL DEFAULT '',
  release_id TEXT NOT NULL DEFAULT '',
  build_id TEXT NOT NULL DEFAULT '',
  job_id TEXT NOT NULL DEFAULT '',
  deployment_id TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_ops_logs_user_time ON ops_logs (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ops_logs_user_trace ON ops_logs (user_id, trace_id);
CREATE INDEX IF NOT EXISTS idx_ops_logs_user_service ON ops_logs (user_id, service_name);
CREATE INDEX IF NOT EXISTS idx_ops_logs_user_release ON ops_logs (user_id, release_id);
CREATE INDEX IF NOT EXISTS idx_ops_logs_user_source ON ops_logs (user_id, source);
CREATE INDEX IF NOT EXISTS idx_ops_logs_user_build ON ops_logs (user_id, build_id);

ALTER TABLE builds ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE compute_jobs ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
