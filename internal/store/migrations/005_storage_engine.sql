-- 005_storage_engine.sql
-- Stage 4.0: file identity + resumable storage transfer bookkeeping

CREATE TABLE IF NOT EXISTS storage_files (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  path TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  transfer_id TEXT,
  bytes_received INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, device_id, path)
);

CREATE INDEX IF NOT EXISTS idx_storage_files_device ON storage_files(user_id, device_id, path);
CREATE INDEX IF NOT EXISTS idx_storage_files_status ON storage_files(user_id, status);

ALTER TABLE transfers ADD COLUMN file_id TEXT;
ALTER TABLE transfers ADD COLUMN resume_offset INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transfers ADD COLUMN is_storage INTEGER NOT NULL DEFAULT 0;
