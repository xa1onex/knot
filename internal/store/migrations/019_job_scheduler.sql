-- Stage 8.5 — scheduler placement, retries, device labels
ALTER TABLE compute_jobs ADD COLUMN placement TEXT NOT NULL DEFAULT 'pinned';
ALTER TABLE compute_jobs ADD COLUMN require_labels TEXT NOT NULL DEFAULT '{}';
ALTER TABLE compute_jobs ADD COLUMN prefer_labels TEXT NOT NULL DEFAULT '{}';
ALTER TABLE compute_jobs ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE compute_jobs ADD COLUMN max_retries INTEGER NOT NULL DEFAULT 0;
ALTER TABLE compute_jobs ADD COLUMN source_path TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS device_labels (
  device_id TEXT PRIMARY KEY,
  labels_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);
