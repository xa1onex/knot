-- Stage 10.5 — plans (proposal only until approve/execute)
CREATE TABLE IF NOT EXISTS plans (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  intent TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  ai_session_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  trace_id TEXT NOT NULL DEFAULT '',
  risk_level TEXT NOT NULL,
  status TEXT NOT NULL,
  input_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  approved_by TEXT NOT NULL DEFAULT '',
  approved_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_plans_user ON plans (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_plans_status ON plans (user_id, status);

CREATE TABLE IF NOT EXISTS plan_steps (
  id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  name TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  risk_level TEXT NOT NULL DEFAULT 'read',
  error TEXT NOT NULL DEFAULT '',
  output_json TEXT NOT NULL DEFAULT '{}',
  trace_id TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  FOREIGN KEY (plan_id) REFERENCES plans(id)
);

CREATE INDEX IF NOT EXISTS idx_plan_steps_plan ON plan_steps (plan_id, seq);

ALTER TABLE audit_events ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_audit_plan ON audit_events (plan_id);
