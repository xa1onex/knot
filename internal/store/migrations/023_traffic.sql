-- Stage 9.4 — Edge traffic binding (blue/green + hostname cutover)
ALTER TABLE edge_routes ADD COLUMN active_release_id TEXT NOT NULL DEFAULT '';
ALTER TABLE edge_routes ADD COLUMN prev_release_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS route_traffic_targets (
  id TEXT PRIMARY KEY,
  route_id TEXT NOT NULL REFERENCES edge_routes(id) ON DELETE CASCADE,
  release_id TEXT NOT NULL,
  weight INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  UNIQUE(route_id, release_id)
);

CREATE INDEX IF NOT EXISTS idx_route_traffic_targets_route ON route_traffic_targets (route_id);

CREATE TABLE IF NOT EXISTS route_traffic_history (
  id TEXT PRIMARY KEY,
  route_id TEXT NOT NULL REFERENCES edge_routes(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  from_release_id TEXT NOT NULL DEFAULT '',
  to_release_id TEXT NOT NULL DEFAULT '',
  weights_json TEXT NOT NULL DEFAULT '{}',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_route_traffic_history_route ON route_traffic_history (route_id, created_at);
