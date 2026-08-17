-- Stage 9.3 — Release management + health gate (pins env/secrets; deploy is a candidate)
CREATE TABLE IF NOT EXISTS releases (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  number INTEGER NOT NULL,
  service TEXT NOT NULL,
  image TEXT NOT NULL,
  environment_id TEXT NOT NULL DEFAULT '',
  environment_name TEXT NOT NULL DEFAULT '',
  config_version TEXT NOT NULL DEFAULT '',
  vars_json TEXT NOT NULL DEFAULT '{}',
  secret_pins_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  device_id TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,
  bind TEXT NOT NULL DEFAULT '127.0.0.1',
  health_path TEXT NOT NULL DEFAULT '/health',
  health_timeout_seconds INTEGER NOT NULL DEFAULT 15,
  health_retries INTEGER NOT NULL DEFAULT 1,
  health_expected_status INTEGER NOT NULL DEFAULT 200,
  hostname TEXT NOT NULL DEFAULT '',
  edge_device_id TEXT NOT NULL DEFAULT '',
  build_id TEXT NOT NULL DEFAULT '',
  job_id TEXT NOT NULL DEFAULT '',
  deployment_id TEXT NOT NULL DEFAULT '',
  prev_release_id TEXT NOT NULL DEFAULT '',
  current INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, service, number)
);

CREATE INDEX IF NOT EXISTS idx_releases_user_service ON releases (user_id, service, number DESC);
CREATE INDEX IF NOT EXISTS idx_releases_current ON releases (user_id, service, current);

CREATE TABLE IF NOT EXISTS release_logs (
  id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
  stream TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'release',
  message TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_release_logs_release ON release_logs (release_id, created_at);

ALTER TABLE deployments ADD COLUMN release_id TEXT NOT NULL DEFAULT '';
