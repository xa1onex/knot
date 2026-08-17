-- Stage 7.2 — public hostname → registered service (metadata; bytes stay on the node)
CREATE TABLE IF NOT EXISTS edge_routes (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hostname TEXT NOT NULL,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  edge_device_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(hostname)
);

CREATE INDEX IF NOT EXISTS idx_edge_routes_user ON edge_routes(user_id);
CREATE INDEX IF NOT EXISTS idx_edge_routes_service ON edge_routes(service_id);
