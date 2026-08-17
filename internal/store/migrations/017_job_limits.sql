-- Stage 8.3 — job resource reasons + disk cap
ALTER TABLE compute_jobs ADD COLUMN disk_mb INTEGER NOT NULL DEFAULT 1024;
ALTER TABLE compute_jobs ADD COLUMN reason TEXT NOT NULL DEFAULT '';
