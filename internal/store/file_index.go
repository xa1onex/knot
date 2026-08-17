package store

import (
	"context"
	"database/sql"
	"path"
	"strings"
	"time"
)

type FileIndexRow struct {
	ID          string
	UserID      string
	DeviceID    string
	DeviceName  string
	Path        string
	Name        string
	Size        int64
	Mtime       string
	SHA256      string
	MimeType    string
	IsDirectory bool
	FileID      string
	IndexedAt   time.Time
}

type FileSearchQuery struct {
	Query          string
	DeviceID       string
	Folder         string // exact parent folder (browse) or prefix when Query is set
	Type           string // image|video|pdf|text|mime prefix
	MinSize        int64
	MaxSize        int64
	ModifiedAfter  string
	ModifiedBefore string
	Directories    *bool
	Limit          int
}

func (s *Store) ReplaceFileIndexForDevice(ctx context.Context, userID, deviceID string, rows []FileIndexRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_index WHERE user_id = ? AND device_id = ?`, userID, deviceID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO file_index (
  id, user_id, device_id, path, name, size, mtime, sha256, mime_type, is_directory, file_id, indexed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := range rows {
		r := &rows[i]
		if r.ID == "" {
			r.ID = NewID()
		}
		if r.Name == "" {
			r.Name = path.Base(r.Path)
		}
		isDir := 0
		if r.IsDirectory {
			isDir = 1
		}
		if _, err := stmt.ExecContext(ctx, r.ID, userID, deviceID, r.Path, r.Name, r.Size, r.Mtime, r.SHA256, r.MimeType, isDir, r.FileID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SearchFileIndex(ctx context.Context, userID string, q FileSearchQuery) ([]FileIndexRow, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	sqlStr := `
SELECT i.id, i.user_id, i.device_id, COALESCE(d.name,''), i.path, i.name, i.size, i.mtime,
       i.sha256, i.mime_type, i.is_directory, i.file_id, i.indexed_at
FROM file_index i
LEFT JOIN devices d ON d.id = i.device_id
WHERE i.user_id = ?`
	args := []any{userID}

	if q.DeviceID != "" {
		sqlStr += ` AND i.device_id = ?`
		args = append(args, q.DeviceID)
	}
	if q.Query != "" {
		like := "%" + likeEscape(q.Query) + "%"
		sqlStr += ` AND (i.name LIKE ? ESCAPE '\' OR i.path LIKE ? ESCAPE '\')`
		args = append(args, like, like)
		if q.Folder != "" {
			sqlStr += ` AND (i.path = ? OR i.path LIKE ?)`
			args = append(args, q.Folder, q.Folder+"/%")
		}
	} else {
		folder := strings.Trim(q.Folder, "/")
		if folder == "" {
			sqlStr += ` AND i.path NOT LIKE '%/%'`
		} else {
			sqlStr += ` AND i.path LIKE ? AND i.path NOT LIKE ?`
			args = append(args, folder+"/%", folder+"/%/%")
		}
	}
	if q.Directories != nil {
		v := 0
		if *q.Directories {
			v = 1
		}
		sqlStr += ` AND i.is_directory = ?`
		args = append(args, v)
	}
	if t := strings.ToLower(strings.TrimSpace(q.Type)); t != "" {
		switch t {
		case "image":
			sqlStr += ` AND i.mime_type LIKE 'image/%'`
		case "video":
			sqlStr += ` AND i.mime_type LIKE 'video/%'`
		case "pdf":
			sqlStr += ` AND (i.mime_type = 'application/pdf' OR i.name LIKE '%.pdf')`
		case "text":
			sqlStr += ` AND (i.mime_type LIKE 'text/%' OR i.mime_type LIKE '%json%' OR i.mime_type LIKE '%xml%')`
		default:
			sqlStr += ` AND i.mime_type LIKE ?`
			args = append(args, t+"%")
		}
	}
	if q.MinSize > 0 {
		sqlStr += ` AND i.size >= ?`
		args = append(args, q.MinSize)
	}
	if q.MaxSize > 0 {
		sqlStr += ` AND i.size <= ?`
		args = append(args, q.MaxSize)
	}
	if q.ModifiedAfter != "" {
		sqlStr += ` AND i.mtime >= ?`
		args = append(args, q.ModifiedAfter)
	}
	if q.ModifiedBefore != "" {
		sqlStr += ` AND i.mtime <= ?`
		args = append(args, q.ModifiedBefore)
	}
	sqlStr += ` ORDER BY i.is_directory DESC, i.name COLLATE NOCASE, i.device_id LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileIndexRow
	for rows.Next() {
		r, err := scanFileIndex(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) ListFileIndexForDevice(ctx context.Context, userID, deviceID string) ([]FileIndexRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT i.id, i.user_id, i.device_id, '', i.path, i.name, i.size, i.mtime,
       i.sha256, i.mime_type, i.is_directory, i.file_id, i.indexed_at
FROM file_index i WHERE i.user_id = ? AND i.device_id = ?`, userID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileIndexRow
	for rows.Next() {
		r, err := scanFileIndex(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) CountFileIndex(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM file_index WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func scanFileIndex(row scanner) (*FileIndexRow, error) {
	var r FileIndexRow
	var isDir int
	var indexed string
	err := row.Scan(
		&r.ID, &r.UserID, &r.DeviceID, &r.DeviceName, &r.Path, &r.Name, &r.Size, &r.Mtime,
		&r.SHA256, &r.MimeType, &isDir, &r.FileID, &indexed,
	)
	if err != nil {
		return nil, err
	}
	r.IsDirectory = isDir != 0
	r.IndexedAt, _ = time.Parse(time.RFC3339Nano, indexed)
	return &r, nil
}

var _ scanner = (*sql.Row)(nil)
