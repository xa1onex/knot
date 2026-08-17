-- Stage 7.6 — login lockout + agent version on devices
CREATE TABLE IF NOT EXISTS login_attempts (
  key TEXT PRIMARY KEY,
  fail_count INTEGER NOT NULL DEFAULT 0,
  locked_until TEXT,
  updated_at TEXT NOT NULL
);

ALTER TABLE devices ADD COLUMN agent_version TEXT DEFAULT '';
