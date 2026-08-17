-- Stage 9.2 — Git application sources + pinned-node Dockerfile builds
CREATE TABLE IF NOT EXISTS app_sources (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type TEXT NOT NULL DEFAULT 'git',
  name TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT 'main',
  git_tag TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL DEFAULT '',
  credential_secret_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_app_sources_user ON app_sources (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS builds (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id TEXT NOT NULL REFERENCES app_sources(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  dockerfile TEXT NOT NULL DEFAULT 'Dockerfile',
  context TEXT NOT NULL DEFAULT '.',
  tag TEXT NOT NULL,
  image TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  git_revision TEXT NOT NULL DEFAULT '',
  registry_secret_id TEXT NOT NULL DEFAULT '',
  timeout_seconds INTEGER NOT NULL DEFAULT 600,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_builds_user ON builds (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_builds_device ON builds (device_id, status);
CREATE INDEX IF NOT EXISTS idx_builds_source ON builds (source_id, created_at DESC);

CREATE TABLE IF NOT EXISTS build_logs (
  id TEXT PRIMARY KEY,
  build_id TEXT NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
  stream TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_build_logs_build ON build_logs (build_id, created_at);
