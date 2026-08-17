package syncjob

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
)

var (
	ErrBusy       = errors.New("sync job already running")
	ErrNotRunning = errors.New("sync job is not running")
	ErrCanceled   = errors.New("sync canceled")
	ErrBadMode    = errors.New("mode must be one_way or two_way")
)

type Service struct {
	Store   *store.Store
	Storage *storage.Service

	mu      sync.Mutex
	running map[string]context.CancelFunc // jobID → cancel
}

func New(st *store.Store, storageSvc *storage.Service) *Service {
	return &Service{
		Store:   st,
		Storage: storageSvc,
		running: make(map[string]context.CancelFunc),
	}
}

type CreateRequest struct {
	UserID         string
	Name           string
	Mode           string
	SourceDeviceID string
	SourcePath     string
	DestDeviceID   string
	DestPath       string
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.SyncJob, error) {
	mode := req.Mode
	if mode == "" {
		mode = store.SyncModeOneWay
	}
	if mode != store.SyncModeOneWay && mode != store.SyncModeTwoWay {
		return nil, ErrBadMode
	}
	src, err := normalizeRoot(req.SourcePath)
	if err != nil {
		return nil, err
	}
	dst, err := normalizeRoot(req.DestPath)
	if err != nil {
		return nil, err
	}
	if req.SourceDeviceID == "" || req.DestDeviceID == "" {
		return nil, fmt.Errorf("source_device_id and dest_device_id required")
	}
	if req.SourceDeviceID == req.DestDeviceID && src == dst {
		return nil, fmt.Errorf("source and destination must differ")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		if mode == store.SyncModeTwoWay {
			name = fmt.Sprintf("%s ↔ %s", src, dst)
		} else {
			name = fmt.Sprintf("%s → %s", src, dst)
		}
	}
	j := &store.SyncJob{
		ID:             store.NewID(),
		UserID:         req.UserID,
		Name:           name,
		Mode:           mode,
		SourceDeviceID: req.SourceDeviceID,
		SourcePath:     src,
		DestDeviceID:   req.DestDeviceID,
		DestPath:       dst,
		Status:         store.SyncIdle,
	}
	if err := s.Store.CreateSyncJob(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.SyncJob, error) {
	return s.Store.GetSyncJob(ctx, userID, id)
}

func (s *Service) List(ctx context.Context, userID string) ([]store.SyncJob, error) {
	return s.Store.ListSyncJobs(ctx, userID)
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	s.mu.Lock()
	if cancel, ok := s.running[id]; ok {
		cancel()
		delete(s.running, id)
	}
	s.mu.Unlock()
	return s.Store.DeleteSyncJob(ctx, userID, id)
}

// Run starts sync in the background. Re-entrant after disconnect/crash.
func (s *Service) Run(ctx context.Context, userID, id string) (*store.SyncJob, error) {
	j, err := s.Store.GetSyncJob(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if j.Mode != store.SyncModeOneWay && j.Mode != store.SyncModeTwoWay {
		return nil, ErrBadMode
	}

	s.mu.Lock()
	if _, busy := s.running[id]; busy {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	if j.Status == store.SyncRunning {
		_ = s.Store.UpdateSyncJobStatus(ctx, id, store.SyncPaused, "interrupted")
		j.Status = store.SyncPaused
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.running[id] = cancel
	s.mu.Unlock()

	if err := s.Store.UpdateSyncJobStatus(ctx, id, store.SyncRunning, ""); err != nil {
		cancel()
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		return nil, err
	}
	j.Status = store.SyncRunning

	go func() {
		var runErr error
		if j.Mode == store.SyncModeTwoWay {
			runErr = s.runTwoWay(runCtx, j)
		} else {
			runErr = s.runOneWay(runCtx, j)
		}
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		if runErr != nil {
			if errors.Is(runErr, ErrCanceled) || runCtx.Err() != nil {
				_ = s.Store.UpdateSyncJobStatus(context.Background(), id, store.SyncPaused, "canceled")
				return
			}
			if errors.Is(runErr, errConflictsRemain) {
				_ = s.Store.UpdateSyncJobStatus(context.Background(), id, store.SyncCompletedWithConflicts, "")
				return
			}
			_ = s.Store.UpdateSyncJobStatus(context.Background(), id, store.SyncFailed, runErr.Error())
			return
		}
		_ = s.Store.UpdateSyncJobStatus(context.Background(), id, store.SyncCompleted, "")
	}()

	return s.Store.GetSyncJob(ctx, userID, id)
}

func (s *Service) Cancel(ctx context.Context, userID, id string) (*store.SyncJob, error) {
	j, err := s.Store.GetSyncJob(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	cancel, ok := s.running[id]
	s.mu.Unlock()
	if !ok && j.Status != store.SyncRunning {
		return nil, ErrNotRunning
	}
	_ = s.Store.UpdateSyncJobStatus(ctx, id, store.SyncCanceling, "")
	if j.CurrentTransferID != "" && s.Storage != nil && s.Storage.Transfers != nil {
		_ = s.Storage.Transfers.Abort(ctx, userID, j.CurrentTransferID, "sync canceled")
	}
	if ok {
		cancel()
	}
	return s.Store.GetSyncJob(ctx, userID, id)
}

type treeEntry struct {
	RelPath string
	IsDir   bool
	Size    int64
	Mtime   string
}

func (s *Service) runOneWay(ctx context.Context, j *store.SyncJob) error {
	srcTree, err := s.walk(ctx, j.UserID, j.SourceDeviceID, j.SourcePath)
	if err != nil {
		return fmt.Errorf("walk source: %w", err)
	}
	var filesTotal, bytesTotal int64
	for _, e := range srcTree {
		if !e.IsDir {
			filesTotal++
			bytesTotal += e.Size
		}
	}
	_ = s.Store.UpdateSyncJobTotals(ctx, j.ID, filesTotal, bytesTotal)
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, 0, 0, "", "")

	// Ensure destination root exists when non-empty.
	if j.DestPath != "" {
		if _, err := s.Storage.Mkdir(ctx, j.UserID, j.DestDeviceID, j.DestPath); err != nil {
			// ignore if exists
			if st, e2 := s.Storage.Stat(ctx, j.UserID, j.DestDeviceID, j.DestPath); e2 != nil || !st.IsDir {
				return fmt.Errorf("ensure dest root: %w", err)
			}
		}
	}

	// Directories first (depth ascending).
	dirs := make([]treeEntry, 0)
	files := make([]treeEntry, 0)
	for _, e := range srcTree {
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sort.Slice(dirs, func(i, k int) bool { return dirs[i].RelPath < dirs[k].RelPath })
	sort.Slice(files, func(i, k int) bool { return files[i].RelPath < files[k].RelPath })

	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		dest := joinRoot(j.DestPath, d.RelPath)
		_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, 0, 0, dest, "")
		if _, err := s.Storage.Mkdir(ctx, j.UserID, j.DestDeviceID, dest); err != nil {
			if st, e2 := s.Storage.Stat(ctx, j.UserID, j.DestDeviceID, dest); e2 != nil || !st.IsDir {
				return fmt.Errorf("mkdir %s: %w", dest, err)
			}
		}
		_ = s.Store.UpsertSyncFileState(ctx, &store.SyncFileState{
			ID: store.NewID(), JobID: j.ID, RelPath: d.RelPath, IsDir: true,
			Mtime: d.Mtime, Status: store.SyncFileSynced,
		})
	}

	var filesDone, bytesDone int64
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		if err := s.syncFile(ctx, j, f, &filesDone, &bytesDone); err != nil {
			return err
		}
	}

	// Mirror deletes: remove dest entries under dest root that are not on source.
	destTree, err := s.walk(ctx, j.UserID, j.DestDeviceID, j.DestPath)
	if err != nil {
		return fmt.Errorf("walk dest: %w", err)
	}
	srcSet := make(map[string]treeEntry, len(srcTree))
	for _, e := range srcTree {
		srcSet[e.RelPath] = e
	}
	// Delete files first, then dirs deepest-first.
	var toDeleteFiles, toDeleteDirs []treeEntry
	for _, e := range destTree {
		if _, ok := srcSet[e.RelPath]; ok {
			continue
		}
		if e.IsDir {
			toDeleteDirs = append(toDeleteDirs, e)
		} else {
			toDeleteFiles = append(toDeleteFiles, e)
		}
	}
	sort.Slice(toDeleteDirs, func(i, k int) bool {
		return len(toDeleteDirs[i].RelPath) > len(toDeleteDirs[k].RelPath)
	})
	for _, e := range toDeleteFiles {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		dest := joinRoot(j.DestPath, e.RelPath)
		_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, filesDone, bytesDone, "delete:"+dest, "")
		if err := s.Storage.Delete(ctx, j.UserID, j.DestDeviceID, dest); err != nil {
			return fmt.Errorf("delete %s: %w", dest, err)
		}
		_ = s.Store.DeleteSyncFileState(ctx, j.ID, e.RelPath)
	}
	for _, e := range toDeleteDirs {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		dest := joinRoot(j.DestPath, e.RelPath)
		_ = s.Storage.Delete(ctx, j.UserID, j.DestDeviceID, dest)
		_ = s.Store.DeleteSyncFileState(ctx, j.ID, e.RelPath)
	}

	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, filesDone, bytesDone, "", "")
	return nil
}

func (s *Service) syncFile(ctx context.Context, j *store.SyncJob, f treeEntry, filesDone, bytesDone *int64) error {
	srcPath := joinRoot(j.SourcePath, f.RelPath)
	dstPath := joinRoot(j.DestPath, f.RelPath)
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, srcPath, "")

	prev, _ := s.Store.GetSyncFileState(ctx, j.ID, f.RelPath)
	needHash := prev == nil || prev.Size != f.Size || prev.Mtime != f.Mtime || prev.SHA256 == ""

	var srcSHA string
	var srcSize int64 = f.Size
	var srcMtime = f.Mtime
	if needHash {
		st, err := s.Storage.Stat(ctx, j.UserID, j.SourceDeviceID, srcPath)
		if err != nil {
			return fmt.Errorf("stat source %s: %w", srcPath, err)
		}
		srcSHA = strings.ToLower(st.SHA256)
		srcSize = st.Size
		srcMtime = st.Mtime
	} else {
		srcSHA = prev.SHA256
	}

	// Skip if dest already matches.
	if dst, err := s.Storage.Stat(ctx, j.UserID, j.DestDeviceID, dstPath); err == nil && !dst.IsDir {
		if strings.EqualFold(dst.SHA256, srcSHA) && dst.Size == srcSize {
			now := time.Now().UTC()
			_ = s.Store.UpsertSyncFileState(ctx, &store.SyncFileState{
				ID: store.NewID(), JobID: j.ID, RelPath: f.RelPath, Size: srcSize,
				Mtime: srcMtime, SHA256: srcSHA, Status: store.SyncFileSynced, LastSyncedAt: &now,
			})
			*filesDone++
			*bytesDone += srcSize
			_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, srcPath, "")
			return nil
		}
	}

	// Fast-path skip using previous state when dest missing check already done above
	// and prev hash matches — still transfer if dest missing or differs.

	if j.SourceDeviceID == j.DestDeviceID {
		if _, err := s.Storage.Copy(ctx, j.UserID, j.SourceDeviceID, srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s → %s: %w", srcPath, dstPath, err)
		}
	} else {
		res, err := s.Storage.TransferBetween(ctx, storage.TransferBetweenRequest{
			UserID:       j.UserID,
			FromDeviceID: j.SourceDeviceID,
			FromPath:     srcPath,
			ToDeviceID:   j.DestDeviceID,
			ToPath:       dstPath,
		})
		if err != nil {
			return fmt.Errorf("transfer %s → %s: %w", srcPath, dstPath, err)
		}
		xferID := res.Transfer.ID
		_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, srcPath, xferID)
		if err := s.waitTransfer(ctx, j.UserID, xferID); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	_ = s.Store.UpsertSyncFileState(ctx, &store.SyncFileState{
		ID: store.NewID(), JobID: j.ID, RelPath: f.RelPath, Size: srcSize,
		Mtime: srcMtime, SHA256: srcSHA, Status: store.SyncFileSynced, LastSyncedAt: &now,
	})
	*filesDone++
	*bytesDone += srcSize
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, srcPath, "")
	return nil
}

func (s *Service) waitTransfer(ctx context.Context, userID, transferID string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			_ = s.Storage.Transfers.Abort(context.Background(), userID, transferID, "sync canceled")
			return ErrCanceled
		}
		t, err := s.Store.GetTransfer(ctx, userID, transferID)
		if err != nil {
			return err
		}
		switch t.Status {
		case store.TransferCompleted:
			return nil
		case store.TransferFailed, store.TransferAborted:
			msg := t.Error
			if msg == "" {
				msg = t.Status
			}
			return fmt.Errorf("transfer %s: %s", transferID, msg)
		}
		select {
		case <-ctx.Done():
			_ = s.Storage.Transfers.Abort(context.Background(), userID, transferID, "sync canceled")
			return ErrCanceled
		case <-ticker.C:
		}
	}
}

func (s *Service) walk(ctx context.Context, userID, deviceID, root string) ([]treeEntry, error) {
	var out []treeEntry
	var walkDir func(rel string) error
	walkDir = func(rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := joinRoot(root, rel)
		ents, err := s.Storage.List(ctx, userID, deviceID, full)
		if err != nil {
			// empty / missing root is ok for dest walk on first sync
			if full == root || rel == "" {
				return nil
			}
			return err
		}
		for _, e := range ents {
			childRel := e.Path
			if root != "" {
				childRel = strings.TrimPrefix(e.Path, root+"/")
				if childRel == e.Path && e.Path == root {
					continue
				}
			}
			// Prefer name join when path not absolute under root
			if childRel == "" || childRel == e.Path {
				if rel == "" {
					childRel = e.Name
				} else {
					childRel = path.Join(rel, e.Name)
				}
			}
			childRel = strings.TrimPrefix(childRel, "./")
			entry := treeEntry{
				RelPath: childRel,
				IsDir:   e.IsDir,
				Size:    e.Size,
				Mtime:   e.Mtime,
			}
			out = append(out, entry)
			if e.IsDir {
				if err := walkDir(childRel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walkDir(""); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeRoot(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if strings.Contains(p, "..") {
		return "", fmt.Errorf("invalid path")
	}
	return p, nil
}

func joinRoot(root, rel string) string {
	rel = strings.Trim(rel, "/")
	root = strings.Trim(root, "/")
	if root == "" {
		return rel
	}
	if rel == "" {
		return root
	}
	return root + "/" + rel
}
