-- Stage 9.1 — environments + encrypted secrets vault (key is not in this DB)
CREATE TABLE IF NOT EXISTS secrets (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_secrets_user ON secrets(user_id);

CREATE TABLE IF NOT EXISTS secret_versions (
  id TEXT PRIMARY KEY,
  secret_id TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  ciphertext TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(secret_id, version)
);

CREATE TABLE IF NOT EXISTS environments (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  vars_json TEXT NOT NULL DEFAULT '{}',
  secrets_json TEXT NOT NULL DEFAULT '{}',
  policy_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, project, name)
);

CREATE INDEX IF NOT EXISTS idx_environments_user ON environments(user_id);

ALTER TABLE deployments ADD COLUMN environment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN secret_pins_json TEXT NOT NULL DEFAULT '{}';
