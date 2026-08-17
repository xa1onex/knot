-- Stage 10.4 — extend existing audit for AI/MCP (no separate log system)
ALTER TABLE audit_events ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user';
ALTER TABLE audit_events ADD COLUMN actor_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN parent_actor TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN ai_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN mcp_client TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN workflow_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_audit_actor_type ON audit_events (user_id, actor_type, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_ai_session ON audit_events (ai_session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_trace ON audit_events (trace_id);
CREATE INDEX IF NOT EXISTS idx_audit_workflow ON audit_events (workflow_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_events (user_id, action, created_at);
