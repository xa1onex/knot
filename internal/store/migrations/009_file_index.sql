-- Stage 6.5 — global file metadata index (no file bytes on Control Plane)
CREATE TABLE IF NOT EXISTS file_index (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  path TEXT NOT NULL,
  name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  mtime TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  mime_type TEXT NOT NULL DEFAULT '',
  is_directory INTEGER NOT NULL DEFAULT 0,
  file_id TEXT NOT NULL DEFAULT '',
  indexed_at TEXT NOT NULL,
  UNIQUE(user_id, device_id, path)
);

CREATE INDEX IF NOT EXISTS idx_file_index_user_name ON file_index(user_id, name);
CREATE INDEX IF NOT EXISTS idx_file_index_user_device ON file_index(user_id, device_id);
CREATE INDEX IF NOT EXISTS idx_file_index_user_path ON file_index(user_id, path);
CREATE INDEX IF NOT EXISTS idx_file_index_user_mime ON file_index(user_id, mime_type);
