package store

import (
	"context"
	"database/sql"
	"time"
)

type Secret struct {
	ID        string
	UserID    string
	Name      string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SecretVersion struct {
	ID         string
	SecretID   string
	Version    int
	Ciphertext string
	CreatedAt  time.Time
}

func (s *Store) CreateSecret(ctx context.Context, sec *Secret, ciphertext string) error {
	now := time.Now().UTC()
	if sec.ID == "" {
		sec.ID = NewID()
	}
	if sec.Version <= 0 {
		sec.Version = 1
	}
	sec.CreatedAt = now
	sec.UpdatedAt = now
	if err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO secrets (id, user_id, name, version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`,
			sec.ID, sec.UserID, sec.Name, sec.Version,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_at)
VALUES (?, ?, ?, ?, ?)`,
			NewID(), sec.ID, sec.Version, ciphertext, now.Format(time.RFC3339Nano))
		return err
	}); err != nil {
		return err
	}
	return nil
}

func (s *Store) RotateSecret(ctx context.Context, userID, id, ciphertext string) (*Secret, error) {
	sec, err := s.GetSecret(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sec.Version++
	sec.UpdatedAt = now
	if err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
UPDATE secrets SET version = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
			sec.Version, now.Format(time.RFC3339Nano), sec.ID, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_at)
VALUES (?, ?, ?, ?, ?)`,
			NewID(), sec.ID, sec.Version, ciphertext, now.Format(time.RFC3339Nano))
		return err
	}); err != nil {
		return nil, err
	}
	return sec, nil
}

func (s *Store) GetSecret(ctx context.Context, userID, id string) (*Secret, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, version, created_at, updated_at FROM secrets WHERE user_id = ? AND id = ?`, userID, id)
	return scanSecret(row)
}

func (s *Store) GetSecretByName(ctx context.Context, userID, name string) (*Secret, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, version, created_at, updated_at FROM secrets WHERE user_id = ? AND name = ?`, userID, name)
	return scanSecret(row)
}

func (s *Store) ListSecrets(ctx context.Context, userID string) ([]Secret, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, name, version, created_at, updated_at FROM secrets WHERE user_id = ? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		sec, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sec)
	}
	if out == nil {
		out = []Secret{}
	}
	return out, rows.Err()
}

func (s *Store) GetSecretVersion(ctx context.Context, secretID string, version int) (*SecretVersion, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, secret_id, version, ciphertext, created_at FROM secret_versions
WHERE secret_id = ? AND version = ?`, secretID, version)
	var v SecretVersion
	var created string
	if err := row.Scan(&v.ID, &v.SecretID, &v.Version, &v.Ciphertext, &created); err != nil {
		return nil, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &v, nil
}

func scanSecret(row scanner) (*Secret, error) {
	var sec Secret
	var created, updated string
	if err := row.Scan(&sec.ID, &sec.UserID, &sec.Name, &sec.Version, &created, &updated); err != nil {
		return nil, err
	}
	sec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	sec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &sec, nil
}
