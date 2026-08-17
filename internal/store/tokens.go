package store

import (
	"context"
	"database/sql"
	"time"
)

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s *Store) CreateRefreshToken(ctx context.Context, t *RefreshToken) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked_at, created_at)
VALUES (?, ?, ?, ?, NULL, ?)`,
		t.ID, t.UserID, t.TokenHash, t.ExpiresAt.Format(time.RFC3339Nano), t.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var t RefreshToken
	var exp, created string
	var rev sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
FROM refresh_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &exp, &rev, &created)
	if err != nil {
		return nil, err
	}
	t.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if rev.Valid {
		u, _ := time.Parse(time.RFC3339Nano, rev.String)
		t.RevokedAt = &u
	}
	return &t, nil
}

func (s *Store) RevokeRefreshToken(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, now(), hash)
	return err
}

func (s *Store) RevokeAllRefreshTokens(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now(), userID)
	return err
}

func (s *Store) DeleteAllSessionsForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) RotateCredential(ctx context.Context, userID, id, newHash, newPrefix string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE credentials SET token_hash = ?, token_prefix = ?, revoked_at = NULL
WHERE id = ? AND user_id = ?`, newHash, newPrefix, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type DeviceSession struct {
	ID        string
	DeviceID  string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s *Store) CreateDeviceSession(ctx context.Context, sess *DeviceSession) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO device_sessions (id, device_id, user_id, token_hash, expires_at, revoked_at, created_at)
VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		sess.ID, sess.DeviceID, sess.UserID, sess.TokenHash,
		sess.ExpiresAt.Format(time.RFC3339Nano), sess.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetDeviceSessionByHash(ctx context.Context, hash string) (*DeviceSession, error) {
	var sess DeviceSession
	var exp, created string
	var rev sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, device_id, user_id, token_hash, expires_at, revoked_at, created_at
FROM device_sessions WHERE token_hash = ?`, hash).
		Scan(&sess.ID, &sess.DeviceID, &sess.UserID, &sess.TokenHash, &exp, &rev, &created)
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if rev.Valid {
		u, _ := time.Parse(time.RFC3339Nano, rev.String)
		sess.RevokedAt = &u
	}
	return &sess, nil
}
