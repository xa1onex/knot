-- 006_storage_quotas.sql
-- Stage 4.1: optional per-credential storage quotas

ALTER TABLE credentials ADD COLUMN max_storage_bytes INTEGER;
ALTER TABLE credentials ADD COLUMN max_file_bytes INTEGER;
ALTER TABLE credentials ADD COLUMN max_files INTEGER;
