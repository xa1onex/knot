package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	TransferPending      = "pending"
	TransferOffered      = "offered"
	TransferNegotiating  = "negotiating"
	TransferTransferring = "transferring"
	TransferCompleted    = "completed"
	TransferFailed       = "failed"
	TransferAborted      = "aborted"
)

type Transfer struct {
	ID            string
	UserID        string
	FromDeviceID  string
	ToDeviceID    string
	Filename      string
	SourcePath    string
	Size          int64
	SHA256        string
	Status        string
	Error         string
	TransportPath string // direct | relay | ""
	FileID        string
	ResumeOffset  int64
	IsStorage     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

func (s *Store) CreateTransfer(ctx context.Context, t *Transfer) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	isStorage := 0
	if t.IsStorage {
		isStorage = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO transfers (id, user_id, from_device_id, to_device_id, filename, source_path, size, sha256, status, error, transport_path, file_id, resume_offset, is_storage, created_at, updated_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		t.ID, t.UserID, t.FromDeviceID, t.ToDeviceID, t.Filename, t.SourcePath, t.Size, t.SHA256,
		t.Status, t.Error, nullStr(t.TransportPath), nullStr(t.FileID), t.ResumeOffset, isStorage,
		t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const transferSelect = `
SELECT id, user_id, from_device_id, to_device_id, filename, source_path, size, sha256, status, COALESCE(error,''), COALESCE(transport_path,''),
 COALESCE(file_id,''), COALESCE(resume_offset,0), COALESCE(is_storage,0), created_at, updated_at, completed_at
FROM transfers`

func (s *Store) GetTransfer(ctx context.Context, userID, id string) (*Transfer, error) {
	row := s.db.QueryRowContext(ctx, transferSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	return scanTransfer(row)
}

func (s *Store) GetTransferByID(ctx context.Context, id string) (*Transfer, error) {
	row := s.db.QueryRowContext(ctx, transferSelect+` WHERE id = ?`, id)
	return scanTransfer(row)
}

func (s *Store) ListTransfers(ctx context.Context, userID string, limit int) ([]Transfer, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, transferSelect+` WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTransferStatus(ctx context.Context, id, status, errMsg string, completed bool) error {
	var completedAt any
	if completed {
		completedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE transfers SET status = ?, error = ?, updated_at = ?, completed_at = COALESCE(?, completed_at)
WHERE id = ?`, status, errMsg, now(), completedAt, id)
	return err
}

func (s *Store) UpdateTransferPath(ctx context.Context, id, path string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE transfers SET transport_path = ?, updated_at = ? WHERE id = ?`, path, now(), id)
	return err
}

// UpdateTransferBytes records how many payload bytes the dest has accepted (progress / resume).
func (s *Store) UpdateTransferBytes(ctx context.Context, id string, bytesReceived int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE transfers SET resume_offset = ?, updated_at = ? WHERE id = ? AND resume_offset < ?`,
		bytesReceived, now(), id, bytesReceived)
	return err
}

func scanTransfer(row scanner) (*Transfer, error) {
	var t Transfer
	var created, updated string
	var completed sql.NullString
	var isStorage int
	if err := row.Scan(&t.ID, &t.UserID, &t.FromDeviceID, &t.ToDeviceID, &t.Filename, &t.SourcePath,
		&t.Size, &t.SHA256, &t.Status, &t.Error, &t.TransportPath, &t.FileID, &t.ResumeOffset, &isStorage,
		&created, &updated, &completed); err != nil {
		return nil, err
	}
	t.IsStorage = isStorage != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if completed.Valid {
		c, _ := time.Parse(time.RFC3339Nano, completed.String)
		t.CompletedAt = &c
	}
	return &t, nil
}
