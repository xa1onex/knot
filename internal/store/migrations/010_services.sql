-- Stage 7.1 — service registry (metadata only; processes stay on the node)
CREATE TABLE IF NOT EXISTS services (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  protocol TEXT NOT NULL,
  port INTEGER NOT NULL,
  bind TEXT NOT NULL DEFAULT '127.0.0.1',
  status TEXT NOT NULL DEFAULT 'registered',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, device_id, name)
);

CREATE INDEX IF NOT EXISTS idx_services_user_device ON services(user_id, device_id);
