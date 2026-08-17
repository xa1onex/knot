package store

import (
	"context"
	"database/sql"
	"time"
)

type LoginAttempt struct {
	Key         string
	FailCount   int
	LockedUntil *time.Time
	UpdatedAt   time.Time
}

func (s *Store) GetLoginAttempt(ctx context.Context, key string) (*LoginAttempt, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT key, fail_count, locked_until, updated_at FROM login_attempts WHERE key = ?`, key)
	var a LoginAttempt
	var locked, updated sql.NullString
	if err := row.Scan(&a.Key, &a.FailCount, &locked, &updated); err != nil {
		return nil, err
	}
	if locked.Valid && locked.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, locked.String)
		a.LockedUntil = &t
	}
	if updated.Valid {
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	}
	return &a, nil
}

func (s *Store) RecordLoginFailure(ctx context.Context, key string, maxFails int, lockFor time.Duration) (locked bool, until *time.Time, err error) {
	now := time.Now().UTC()
	a, err := s.GetLoginAttempt(ctx, key)
	if err != nil && !IsNotFound(err) {
		return false, nil, err
	}
	fails := 1
	if a != nil {
		fails = a.FailCount + 1
		if a.LockedUntil != nil && now.Before(*a.LockedUntil) {
			return true, a.LockedUntil, nil
		}
	}
	var lockedUntil any
	if fails >= maxFails {
		t := now.Add(lockFor)
		until = &t
		locked = true
		lockedUntil = t.Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO login_attempts (key, fail_count, locked_until, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET fail_count = excluded.fail_count, locked_until = excluded.locked_until, updated_at = excluded.updated_at`,
		key, fails, lockedUntil, now.Format(time.RFC3339Nano))
	return locked, until, err
}

func (s *Store) ClearLoginAttempts(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_attempts WHERE key = ?`, key)
	return err
}

func (s *Store) IsLoginLocked(ctx context.Context, key string) (bool, *time.Time, error) {
	a, err := s.GetLoginAttempt(ctx, key)
	if err != nil {
		if IsNotFound(err) {
			return false, nil, nil
		}
		return false, nil, err
	}
	if a.LockedUntil == nil {
		return false, nil, nil
	}
	if time.Now().UTC().Before(*a.LockedUntil) {
		return true, a.LockedUntil, nil
	}
	return false, nil, nil
}

func (s *Store) BackupSQLite(ctx context.Context, destPath string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, destPath)
	return err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) UpdateDeviceAgentVersion(ctx context.Context, deviceID, version string) error {
	if version == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET agent_version = ? WHERE id = ?`, version, deviceID)
	return err
}
