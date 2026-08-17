package offline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const metaBaseline = "baseline_v1"

// baselineEntry is the last known on-disk snapshot for a relative path.
type baselineEntry struct {
	FileID string `json:"file_id"`
	Size   int64  `json:"size"`
	Mtime  string `json:"mtime"`
	SHA256 string `json:"sha256"`
	IsDir  bool   `json:"is_dir"`
}

// Scanner diffs storage root against a durable baseline and appends journal entries.
type Scanner struct {
	Root  string
	Queue *Queue
}

func NewScanner(root string, q *Queue) *Scanner {
	return &Scanner{Root: root, Queue: q}
}

// LoadBaseline returns the last saved map (path → state).
func (s *Scanner) LoadBaseline(ctx context.Context) (map[string]baselineEntry, error) {
	raw, err := s.Queue.GetMeta(ctx, metaBaseline)
	if err != nil {
		return nil, err
	}
	out := map[string]baselineEntry{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]baselineEntry{}, nil
	}
	return out, nil
}

func (s *Scanner) saveBaseline(ctx context.Context, base map[string]baselineEntry) error {
	b, err := json.Marshal(base)
	if err != nil {
		return err
	}
	return s.Queue.SetMeta(ctx, metaBaseline, string(b))
}

// SeedBaseline walks the tree and stores it without enqueueing (first online / install).
func (s *Scanner) SeedBaseline(ctx context.Context) error {
	cur, err := s.walk()
	if err != nil {
		return err
	}
	return s.saveBaseline(ctx, cur)
}

// ScanOnce compares FS to baseline, appends PENDING journal rows, updates baseline.
func (s *Scanner) ScanOnce(ctx context.Context) (int, error) {
	prev, err := s.LoadBaseline(ctx)
	if err != nil {
		return 0, err
	}
	cur, err := s.walk()
	if err != nil {
		return 0, err
	}

	added := 0
	deleted := map[string]baselineEntry{}
	created := map[string]baselineEntry{}

	for path, old := range prev {
		neu, ok := cur[path]
		if !ok {
			deleted[path] = old
			continue
		}
		if old.IsDir != neu.IsDir {
			deleted[path] = old
			created[path] = neu
			continue
		}
		if old.IsDir {
			continue
		}
		if old.Size != neu.Size || old.Mtime != neu.Mtime || old.SHA256 != neu.SHA256 {
			if old.SHA256 != neu.SHA256 {
				fileID := old.FileID
				if fileID == "" {
					fileID = neu.FileID
				}
				neu.FileID = fileID
				cur[path] = neu
				oldS := FileState{Path: path, FileID: old.FileID, Size: old.Size, Mtime: old.Mtime, SHA256: old.SHA256}
				newS := FileState{Path: path, FileID: fileID, Size: neu.Size, Mtime: neu.Mtime, SHA256: neu.SHA256}
				if _, err := s.Queue.Append(ctx, OpModify, path, fileID, oldS, newS); err != nil {
					return added, err
				}
				added++
			}
		} else {
			neu.FileID = old.FileID
			cur[path] = neu
		}
	}
	for path, neu := range cur {
		if _, ok := prev[path]; !ok {
			created[path] = neu
		}
	}

	renamedFrom := map[string]string{}
	for dPath, dEnt := range deleted {
		if dEnt.IsDir || dEnt.SHA256 == "" {
			continue
		}
		for cPath, cEnt := range created {
			if cEnt.IsDir || cEnt.SHA256 != dEnt.SHA256 || cEnt.Size != dEnt.Size {
				continue
			}
			if _, used := renamedFrom[cPath]; used {
				continue
			}
			renamedFrom[cPath] = dPath
			break
		}
	}

	for cPath, dPath := range renamedFrom {
		dEnt := deleted[dPath]
		cEnt := created[cPath]
		fileID := dEnt.FileID
		if fileID == "" {
			fileID = cEnt.FileID
		}
		cEnt.FileID = fileID
		cur[cPath] = cEnt
		oldS := FileState{Path: dPath, FileID: dEnt.FileID, Size: dEnt.Size, Mtime: dEnt.Mtime, SHA256: dEnt.SHA256}
		newS := FileState{Path: cPath, FileID: fileID, Size: cEnt.Size, Mtime: cEnt.Mtime, SHA256: cEnt.SHA256}
		if _, err := s.Queue.Append(ctx, OpRename, cPath, fileID, oldS, newS); err != nil {
			return added, err
		}
		added++
		delete(deleted, dPath)
		delete(created, cPath)
	}

	for path, old := range deleted {
		oldS := FileState{Path: path, FileID: old.FileID, Size: old.Size, Mtime: old.Mtime, SHA256: old.SHA256}
		newS := FileState{Path: path, FileID: old.FileID, Deleted: true}
		if _, err := s.Queue.Append(ctx, OpDelete, path, old.FileID, oldS, newS); err != nil {
			return added, err
		}
		added++
	}
	for path, neu := range created {
		if neu.IsDir {
			continue
		}
		fileID := neu.FileID
		if fileID == "" {
			fileID = uuid.NewString()
			neu.FileID = fileID
			cur[path] = neu
		}
		oldS := FileState{Path: path, Deleted: true}
		newS := FileState{Path: path, FileID: fileID, Size: neu.Size, Mtime: neu.Mtime, SHA256: neu.SHA256}
		if _, err := s.Queue.Append(ctx, OpCreate, path, fileID, oldS, newS); err != nil {
			return added, err
		}
		added++
	}

	if err := s.saveBaseline(ctx, cur); err != nil {
		return added, err
	}
	return added, nil
}

// Loop scans periodically while shouldRun() is true.
func (s *Scanner) Loop(ctx context.Context, interval time.Duration, shouldRun func() bool) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if shouldRun == nil || !shouldRun() {
				continue
			}
			_, _ = s.ScanOnce(ctx)
		}
	}
}

func (s *Scanner) walk() (map[string]baselineEntry, error) {
	out := map[string]baselineEntry{}
	err := filepath.WalkDir(s.Root, func(abs string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Root, abs)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if shouldSkip(rel, d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		ent := baselineEntry{
			Size:  info.Size(),
			Mtime: info.ModTime().UTC().Format(time.RFC3339Nano),
			IsDir: d.IsDir(),
		}
		if !d.IsDir() {
			sum, err := fileSHA256(abs)
			if err != nil {
				return err
			}
			ent.SHA256 = sum
			ent.FileID = uuid.NewString()
		}
		out[rel] = ent
		return nil
	})
	return out, err
}

func shouldSkip(rel, name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if strings.Contains(rel, ".knot.part") {
		return true
	}
	return false
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
