-- 003_transfers.sql
CREATE TABLE IF NOT EXISTS transfers (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  from_device_id TEXT NOT NULL,
  to_device_id TEXT NOT NULL,
  filename TEXT NOT NULL,
  source_path TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_transfers_user ON transfers(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_transfers_status ON transfers(status);
