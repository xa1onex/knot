package store

import (
	"context"
	"time"
)

const (
	FileUploading  = "uploading"
	FileIncomplete = "incomplete"
	FileComplete   = "complete"
)

type StorageFile struct {
	ID            string
	UserID        string
	DeviceID      string
	Path          string
	Size          int64
	SHA256        string
	Status        string
	TransferID    string
	BytesReceived int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Store) UpsertStorageFile(ctx context.Context, f *StorageFile) error {
	existing, err := s.GetStorageFileByPath(ctx, f.UserID, f.DeviceID, f.Path)
	if err == nil {
		// If caller supplied a new ID (fresh upload), replace the path binding.
		if f.ID != "" && f.ID != existing.ID {
			_, err = s.db.ExecContext(ctx, `DELETE FROM storage_files WHERE id = ?`, existing.ID)
			if err != nil {
				return err
			}
			// fall through to insert
		} else {
			f.ID = existing.ID
			f.CreatedAt = existing.CreatedAt
			f.UpdatedAt = time.Now().UTC()
			_, err = s.db.ExecContext(ctx, `
UPDATE storage_files SET size = ?, sha256 = ?, status = ?, transfer_id = ?, bytes_received = ?, updated_at = ?
WHERE id = ?`, f.Size, f.SHA256, f.Status, nullStr(f.TransferID), f.BytesReceived,
				f.UpdatedAt.Format(time.RFC3339Nano), f.ID)
			return err
		}
	} else if !IsNotFound(err) {
		return err
	}
	if f.ID == "" {
		f.ID = NewID()
	}
	nowt := time.Now().UTC()
	f.CreatedAt = nowt
	f.UpdatedAt = nowt
	_, err = s.db.ExecContext(ctx, `
INSERT INTO storage_files (id, user_id, device_id, path, size, sha256, status, transfer_id, bytes_received, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.UserID, f.DeviceID, f.Path, f.Size, f.SHA256, f.Status, nullStr(f.TransferID), f.BytesReceived,
		f.CreatedAt.Format(time.RFC3339Nano), f.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetStorageFile(ctx context.Context, userID, id string) (*StorageFile, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, device_id, path, size, sha256, status, COALESCE(transfer_id,''), bytes_received, created_at, updated_at
FROM storage_files WHERE user_id = ? AND id = ?`, userID, id)
	return scanStorageFile(row)
}

func (s *Store) GetStorageFileByPath(ctx context.Context, userID, deviceID, path string) (*StorageFile, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, device_id, path, size, sha256, status, COALESCE(transfer_id,''), bytes_received, created_at, updated_at
FROM storage_files WHERE user_id = ? AND device_id = ? AND path = ?`, userID, deviceID, path)
	return scanStorageFile(row)
}

func (s *Store) UpdateStorageFileProgress(ctx context.Context, id string, bytesReceived int64, transferID, status string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE storage_files SET bytes_received = ?, transfer_id = ?, status = ?, updated_at = ? WHERE id = ?`,
		bytesReceived, nullStr(transferID), status, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) CompleteStorageFile(ctx context.Context, id string, size int64, sha256 string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE storage_files SET size = ?, sha256 = ?, status = ?, bytes_received = ?, updated_at = ? WHERE id = ?`,
		size, sha256, FileComplete, size, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) MarkStorageFileIncomplete(ctx context.Context, id string, bytesReceived int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE storage_files SET status = ?, bytes_received = ?, updated_at = ? WHERE id = ?`,
		FileIncomplete, bytesReceived, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) DeleteStorageFileByPath(ctx context.Context, userID, deviceID, path string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_files WHERE user_id = ? AND device_id = ? AND path = ?`, userID, deviceID, path)
	return err
}

func (s *Store) UpdateStorageFilePath(ctx context.Context, id, newPath string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE storage_files SET path = ?, updated_at = ? WHERE id = ?`,
		newPath, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) StorageUsage(ctx context.Context, userID, deviceID string) (totalBytes int64, fileCount int64, err error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(size),0), COUNT(*) FROM storage_files
WHERE user_id = ? AND device_id = ? AND status = ?`, userID, deviceID, FileComplete)
	err = row.Scan(&totalBytes, &fileCount)
	return
}

func scanStorageFile(row scanner) (*StorageFile, error) {
	var f StorageFile
	var created, updated string
	err := row.Scan(&f.ID, &f.UserID, &f.DeviceID, &f.Path, &f.Size, &f.SHA256, &f.Status, &f.TransferID, &f.BytesReceived, &created, &updated)
	if err != nil {
		return nil, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &f, nil
}