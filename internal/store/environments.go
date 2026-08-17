package store

import (
	"context"
	"database/sql"
	"time"
)

type Environment struct {
	ID          string
	UserID      string
	Project     string
	Name        string
	VarsJSON    string
	SecretsJSON string
	PolicyJSON  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (s *Store) CreateEnvironment(ctx context.Context, e *Environment) error {
	now := time.Now().UTC()
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.VarsJSON == "" {
		e.VarsJSON = "{}"
	}
	if e.SecretsJSON == "" {
		e.SecretsJSON = "{}"
	}
	if e.PolicyJSON == "" {
		e.PolicyJSON = "{}"
	}
	e.CreatedAt = now
	e.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO environments (id, user_id, project, name, vars_json, secrets_json, policy_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.UserID, e.Project, e.Name, e.VarsJSON, e.SecretsJSON, e.PolicyJSON,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateEnvironment(ctx context.Context, e *Environment) error {
	e.UpdatedAt = time.Now().UTC()
	if e.VarsJSON == "" {
		e.VarsJSON = "{}"
	}
	if e.SecretsJSON == "" {
		e.SecretsJSON = "{}"
	}
	if e.PolicyJSON == "" {
		e.PolicyJSON = "{}"
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE environments SET vars_json = ?, secrets_json = ?, policy_json = ?, updated_at = ?
WHERE id = ? AND user_id = ?`,
		e.VarsJSON, e.SecretsJSON, e.PolicyJSON, e.UpdatedAt.Format(time.RFC3339Nano), e.ID, e.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetEnvironment(ctx context.Context, userID, id string) (*Environment, error) {
	row := s.db.QueryRowContext(ctx, environmentSelect+` WHERE user_id = ? AND id = ?`, userID, id)
	return scanEnvironment(row)
}

func (s *Store) GetEnvironmentByName(ctx context.Context, userID, project, name string) (*Environment, error) {
	row := s.db.QueryRowContext(ctx, environmentSelect+` WHERE user_id = ? AND project = ? AND name = ?`, userID, project, name)
	return scanEnvironment(row)
}

func (s *Store) ListEnvironments(ctx context.Context, userID, project string) ([]Environment, error) {
	q := environmentSelect + ` WHERE user_id = ?`
	args := []any{userID}
	if project != "" {
		q += ` AND project = ?`
		args = append(args, project)
	}
	q += ` ORDER BY project COLLATE NOCASE, name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	if out == nil {
		out = []Environment{}
	}
	return out, rows.Err()
}

const environmentSelect = `
SELECT id, user_id, project, name, vars_json, secrets_json, policy_json, created_at, updated_at
FROM environments`

func scanEnvironment(row scanner) (*Environment, error) {
	var e Environment
	var created, updated string
	if err := row.Scan(&e.ID, &e.UserID, &e.Project, &e.Name, &e.VarsJSON, &e.SecretsJSON, &e.PolicyJSON, &created, &updated); err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &e, nil
}
