-- Stage 10.3 — scoped AI sessions reuse credentials (kind=temporary_ai)
ALTER TABLE credentials ADD COLUMN kind TEXT NOT NULL DEFAULT 'api';

CREATE INDEX IF NOT EXISTS idx_credentials_user_kind ON credentials (user_id, kind);
