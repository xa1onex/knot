-- Stage 7.4 — structured deployments (metadata on CP; workload runs on agent)
CREATE TABLE IF NOT EXISTS deployments (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  service_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  runtime TEXT NOT NULL DEFAULT 'docker',
  image TEXT NOT NULL,
  port INTEGER NOT NULL,
  bind TEXT NOT NULL DEFAULT '127.0.0.1',
  env_json TEXT NOT NULL DEFAULT '{}',
  health_path TEXT NOT NULL DEFAULT '/',
  revision INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'pending',
  container_id TEXT NOT NULL DEFAULT '',
  prev_deployment_id TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 0,
  health_ok INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployments_user ON deployments(user_id);
CREATE INDEX IF NOT EXISTS idx_deployments_device ON deployments(device_id);
CREATE INDEX IF NOT EXISTS idx_deployments_active ON deployments(user_id, device_id, name, active);

CREATE TABLE IF NOT EXISTS deployment_logs (
  id TEXT PRIMARY KEY,
  deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
  stream TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_logs_dep ON deployment_logs(deployment_id);
