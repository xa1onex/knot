-- Stage 8.1 — Compute Registry snapshots (last telemetry per device)
CREATE TABLE IF NOT EXISTS device_compute (
  device_id TEXT PRIMARY KEY,
  snapshot_json TEXT NOT NULL,
  collected_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
