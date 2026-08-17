package syncjob

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
)

type sideSnap struct {
	Exists bool
	IsDir  bool
	Size   int64
	Mtime  string
	SHA256 string
	FileID string
}

func (s *Service) runTwoWay(ctx context.Context, j *store.SyncJob) error {
	if err := s.ensureRoot(ctx, j.UserID, j.SourceDeviceID, j.SourcePath); err != nil {
		return err
	}
	if err := s.ensureRoot(ctx, j.UserID, j.DestDeviceID, j.DestPath); err != nil {
		return err
	}

	aTree, err := s.walk(ctx, j.UserID, j.SourceDeviceID, j.SourcePath)
	if err != nil {
		return fmt.Errorf("walk A: %w", err)
	}
	bTree, err := s.walk(ctx, j.UserID, j.DestDeviceID, j.DestPath)
	if err != nil {
		return fmt.Errorf("walk B: %w", err)
	}
	aMap := indexTree(aTree)
	bMap := indexTree(bTree)

	states, err := s.Store.ListSyncFileStates(ctx, j.ID)
	if err != nil {
		return err
	}
	stateMap := make(map[string]store.SyncFileState, len(states))
	for _, st := range states {
		stateMap[st.RelPath] = st
	}

	paths := map[string]struct{}{}
	for p := range aMap {
		paths[p] = struct{}{}
	}
	for p := range bMap {
		paths[p] = struct{}{}
	}
	for p, st := range stateMap {
		if !st.Deleted || st.Status == store.SyncFileConflict {
			paths[p] = struct{}{}
		}
		if st.Deleted {
			paths[p] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)

	// Dirs first for mkdir propagation.
	sort.SliceStable(ordered, func(i, k int) bool {
		ai, ak := aMap[ordered[i]], aMap[ordered[k]]
		bi, bk := bMap[ordered[i]], bMap[ordered[k]]
		di := (ai != nil && ai.IsDir) || (bi != nil && bi.IsDir)
		dk := (ak != nil && ak.IsDir) || (bk != nil && bk.IsDir)
		if di != dk {
			return di && !dk
		}
		return ordered[i] < ordered[k]
	})

	var filesTotal, bytesTotal int64
	for _, p := range ordered {
		if e := aMap[p]; e != nil && !e.IsDir {
			filesTotal++
			bytesTotal += e.Size
		} else if e := bMap[p]; e != nil && !e.IsDir {
			filesTotal++
			bytesTotal += e.Size
		}
	}
	_ = s.Store.UpdateSyncJobTotals(ctx, j.ID, filesTotal, bytesTotal)

	var filesDone, bytesDone int64
	for _, rel := range ordered {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		if err := s.reconcilePath(ctx, j, rel, aMap[rel], bMap[rel], stateMap[rel], &filesDone, &bytesDone); err != nil {
			return err
		}
	}

	n, _ := s.Store.CountOpenSyncConflicts(ctx, j.ID)
	_ = s.Store.UpdateSyncJobConflictsOpen(ctx, j.ID, n)
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, filesDone, bytesDone, "", "")
	if n > 0 {
		return errConflictsRemain
	}
	return nil
}

var errConflictsRemain = fmt.Errorf("sync finished with open conflicts")

func indexTree(ents []treeEntry) map[string]*treeEntry {
	m := make(map[string]*treeEntry, len(ents))
	for i := range ents {
		e := ents[i]
		m[e.RelPath] = &e
	}
	return m
}

func (s *Service) ensureRoot(ctx context.Context, userID, deviceID, root string) error {
	if root == "" {
		return nil
	}
	if _, err := s.Storage.Mkdir(ctx, userID, deviceID, root); err != nil {
		if st, e2 := s.Storage.Stat(ctx, userID, deviceID, root); e2 != nil || !st.IsDir {
			return fmt.Errorf("ensure root %s: %w", root, err)
		}
	}
	return nil
}

func (s *Service) reconcilePath(
	ctx context.Context,
	j *store.SyncJob,
	rel string,
	aEnt, bEnt *treeEntry,
	base store.SyncFileState,
	filesDone, bytesDone *int64,
) error {
	// Skip unresolved conflicts until user resolves.
	if open, err := s.Store.GetOpenSyncConflictByPath(ctx, j.ID, rel); err == nil && open != nil {
		_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, "conflict:"+rel, "")
		return nil
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}

	a := s.snapFromEntry(aEnt)
	b := s.snapFromEntry(bEnt)
	hasBase := base.JobID != "" && (base.SHA256 != "" || base.Deleted || base.IsDir)

	// Directories: ensure both sides have them when either/base says so.
	if (aEnt != nil && aEnt.IsDir) || (bEnt != nil && bEnt.IsDir) || (hasBase && base.IsDir && !base.Deleted) {
		return s.reconcileDir(ctx, j, rel, aEnt, bEnt, base)
	}

	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, rel, "")

	aChanged, err := s.sideChanged(ctx, j.UserID, j.SourceDeviceID, joinRoot(j.SourcePath, rel), &a, base, hasBase)
	if err != nil {
		return err
	}
	bChanged, err := s.sideChanged(ctx, j.UserID, j.DestDeviceID, joinRoot(j.DestPath, rel), &b, base, hasBase)
	if err != nil {
		return err
	}

	aDel := hasBase && !base.Deleted && !a.Exists
	bDel := hasBase && !base.Deleted && !b.Exists
	aCreate := a.Exists && (!hasBase || base.Deleted)
	bCreate := b.Exists && (!hasBase || base.Deleted)

	// Identical content on both sides (including both created same / both modified same).
	if a.Exists && b.Exists && a.SHA256 != "" && strings.EqualFold(a.SHA256, b.SHA256) {
		return s.markSynced(ctx, j.ID, rel, a, false, filesDone, bytesDone)
	}

	switch {
	case aChanged && !bChanged && !bDel:
		// A wins → push to B (create/modify) or delete B if A deleted
		if aDel || (!a.Exists && hasBase && !base.Deleted) {
			return s.applyDelete(ctx, j, "B", rel, filesDone, bytesDone)
		}
		if a.Exists {
			if err := s.pushFile(ctx, j, "A", "B", rel, a, filesDone, bytesDone); err != nil {
				return err
			}
			return nil
		}
	case bChanged && !aChanged && !aDel:
		if bDel || (!b.Exists && hasBase && !base.Deleted) {
			return s.applyDelete(ctx, j, "A", rel, filesDone, bytesDone)
		}
		if b.Exists {
			if err := s.pushFile(ctx, j, "B", "A", rel, b, filesDone, bytesDone); err != nil {
				return err
			}
			return nil
		}
	case aCreate && !b.Exists:
		return s.pushFile(ctx, j, "A", "B", rel, a, filesDone, bytesDone)
	case bCreate && !a.Exists:
		return s.pushFile(ctx, j, "B", "A", rel, b, filesDone, bytesDone)
	case aDel && bDel:
		return s.markSynced(ctx, j.ID, rel, sideSnap{}, true, filesDone, bytesDone)
	case aDel && !bChanged && b.Exists && hasBase && sameAsBase(b, base):
		return s.applyDelete(ctx, j, "B", rel, filesDone, bytesDone)
	case bDel && !aChanged && a.Exists && hasBase && sameAsBase(a, base):
		return s.applyDelete(ctx, j, "A", rel, filesDone, bytesDone)
	case !aChanged && !bChanged:
		if a.Exists || b.Exists {
			snap := a
			if !snap.Exists {
				snap = b
			}
			return s.markSynced(ctx, j.ID, rel, snap, false, filesDone, bytesDone)
		}
		if hasBase {
			return s.markSynced(ctx, j.ID, rel, sideSnap{}, true, filesDone, bytesDone)
		}
		return nil
	}

	// Concurrent changes / delete-vs-modify → conflict
	if (aChanged && bChanged) || (aDel && bChanged) || (bDel && aChanged) || (aCreate && bCreate) ||
		(aDel && b.Exists && !sameAsBase(b, base)) || (bDel && a.Exists && !sameAsBase(a, base)) {
		return s.openConflict(ctx, j, rel, a, b, base, hasBase)
	}

	// Fallback: if only one side exists, push it
	if a.Exists && !b.Exists {
		return s.pushFile(ctx, j, "A", "B", rel, a, filesDone, bytesDone)
	}
	if b.Exists && !a.Exists {
		return s.pushFile(ctx, j, "B", "A", rel, b, filesDone, bytesDone)
	}
	return nil
}

func (s *Service) reconcileDir(ctx context.Context, j *store.SyncJob, rel string, aEnt, bEnt *treeEntry, base store.SyncFileState) error {
	needA := aEnt == nil || !aEnt.IsDir
	needB := bEnt == nil || !bEnt.IsDir
	// If one side deleted the dir and the other still has it with only synced content — handled at file level.
	// Ensure mkdir on missing side when the other has the dir.
	if aEnt != nil && aEnt.IsDir && needB {
		if _, err := s.Storage.Mkdir(ctx, j.UserID, j.DestDeviceID, joinRoot(j.DestPath, rel)); err != nil {
			return err
		}
	}
	if bEnt != nil && bEnt.IsDir && needA {
		if _, err := s.Storage.Mkdir(ctx, j.UserID, j.SourceDeviceID, joinRoot(j.SourcePath, rel)); err != nil {
			return err
		}
	}
	mtime := ""
	if aEnt != nil {
		mtime = aEnt.Mtime
	} else if bEnt != nil {
		mtime = bEnt.Mtime
	}
	now := time.Now().UTC()
	return s.Store.UpsertSyncFileState(ctx, &store.SyncFileState{
		ID: store.NewID(), JobID: j.ID, RelPath: rel, IsDir: true, Status: store.SyncFileSynced,
		Mtime: mtime, LastSyncedAt: &now, CreatedAt: base.CreatedAt,
	})
}

func (s *Service) snapFromEntry(e *treeEntry) sideSnap {
	if e == nil {
		return sideSnap{}
	}
	return sideSnap{Exists: true, IsDir: e.IsDir, Size: e.Size, Mtime: e.Mtime}
}

func sameAsBase(snap sideSnap, base store.SyncFileState) bool {
	if !snap.Exists {
		return base.Deleted
	}
	if base.Deleted {
		return false
	}
	if base.SHA256 != "" && snap.SHA256 != "" {
		return strings.EqualFold(snap.SHA256, base.SHA256) && snap.Size == base.Size
	}
	return snap.Size == base.Size && snap.Mtime == base.Mtime
}

// sideChanged fills SHA256 when metadata differs from base. Returns whether content changed vs last sync.
func (s *Service) sideChanged(ctx context.Context, userID, deviceID, fullPath string, snap *sideSnap, base store.SyncFileState, hasBase bool) (bool, error) {
	if !snap.Exists {
		if !hasBase || base.Deleted {
			return false, nil // still absent
		}
		return true, nil // deleted since last sync
	}
	if snap.IsDir {
		return false, nil
	}
	if !hasBase || base.Deleted {
		// new file — need hash for conflict comparison
		st, err := s.Storage.Stat(ctx, userID, deviceID, fullPath)
		if err != nil {
			return false, err
		}
		snap.SHA256 = strings.ToLower(st.SHA256)
		snap.Size = st.Size
		snap.Mtime = st.Mtime
		snap.FileID = st.FileID
		return true, nil
	}
	// Fast candidate: size+mtime match → unchanged (no hash)
	if snap.Size == base.Size && snap.Mtime == base.Mtime && base.SHA256 != "" {
		snap.SHA256 = base.SHA256
		snap.FileID = base.FileID
		return false, nil
	}
	st, err := s.Storage.Stat(ctx, userID, deviceID, fullPath)
	if err != nil {
		return false, err
	}
	snap.SHA256 = strings.ToLower(st.SHA256)
	snap.Size = st.Size
	snap.Mtime = st.Mtime
	snap.FileID = st.FileID
	if strings.EqualFold(snap.SHA256, base.SHA256) && snap.Size == base.Size {
		return false, nil // metadata drift only
	}
	return true, nil
}

func (s *Service) pushFile(ctx context.Context, j *store.SyncJob, fromSide, toSide, rel string, src sideSnap, filesDone, bytesDone *int64) error {
	fromDev, fromRoot, toDev, toRoot := j.SourceDeviceID, j.SourcePath, j.DestDeviceID, j.DestPath
	if fromSide == "B" {
		fromDev, fromRoot, toDev, toRoot = j.DestDeviceID, j.DestPath, j.SourceDeviceID, j.SourcePath
	}
	fromPath := joinRoot(fromRoot, rel)
	toPath := joinRoot(toRoot, rel)
	// Ensure parent dirs on destination
	if dir := path.Dir(rel); dir != "." && dir != "" {
		if _, err := s.Storage.Mkdir(ctx, j.UserID, toDev, joinRoot(toRoot, dir)); err != nil {
			if st, e2 := s.Storage.Stat(ctx, j.UserID, toDev, joinRoot(toRoot, dir)); e2 != nil || !st.IsDir {
				return fmt.Errorf("mkdir parent: %w", err)
			}
		}
	}
	if err := s.transferOrCopy(ctx, j, fromDev, fromPath, toDev, toPath); err != nil {
		return err
	}
	return s.markSynced(ctx, j.ID, rel, src, false, filesDone, bytesDone)
}

func (s *Service) applyDelete(ctx context.Context, j *store.SyncJob, side, rel string, filesDone, bytesDone *int64) error {
	dev, root := j.DestDeviceID, j.DestPath
	if side == "A" {
		dev, root = j.SourceDeviceID, j.SourcePath
	}
	full := joinRoot(root, rel)
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, *filesDone, *bytesDone, "delete:"+full, "")
	if err := s.Storage.Delete(ctx, j.UserID, dev, full); err != nil {
		// already gone is fine
		if _, e2 := s.Storage.Stat(ctx, j.UserID, dev, full); e2 == nil {
			return fmt.Errorf("delete %s: %w", full, err)
		}
	}
	return s.markSynced(ctx, j.ID, rel, sideSnap{}, true, filesDone, bytesDone)
}

func (s *Service) markSynced(ctx context.Context, jobID, rel string, snap sideSnap, deleted bool, filesDone, bytesDone *int64) error {
	now := time.Now().UTC()
	st := &store.SyncFileState{
		ID: store.NewID(), JobID: jobID, RelPath: rel,
		FileID: snap.FileID, Size: snap.Size, Mtime: snap.Mtime, SHA256: strings.ToLower(snap.SHA256),
		IsDir: snap.IsDir, Deleted: deleted, Status: store.SyncFileSynced, LastSyncedAt: &now,
	}
	if deleted {
		st.Status = store.SyncFileDeleted
		st.SHA256 = ""
		st.Size = 0
	}
	if err := s.Store.UpsertSyncFileState(ctx, st); err != nil {
		return err
	}
	if !deleted && !snap.IsDir {
		*filesDone++
		*bytesDone += snap.Size
	}
	_ = s.Store.UpdateSyncJobProgress(ctx, jobID, *filesDone, *bytesDone, rel, "")
	return nil
}

func (s *Service) openConflict(ctx context.Context, j *store.SyncJob, rel string, a, b sideSnap, base store.SyncFileState, hasBase bool) error {
	c := &store.SyncConflict{
		ID: store.NewID(), JobID: j.ID, RelPath: rel, Status: store.SyncConflictOpen,
		AExists: a.Exists, ADeleted: !a.Exists && hasBase && !base.Deleted,
		ASize: a.Size, AMtime: a.Mtime, ASHA256: a.SHA256,
		BExists: b.Exists, BDeleted: !b.Exists && hasBase && !base.Deleted,
		BSize: b.Size, BMtime: b.Mtime, BSHA256: b.SHA256,
	}
	if hasBase {
		c.BaseSHA256 = base.SHA256
		c.BaseSize = base.Size
	}
	if err := s.Store.UpsertSyncConflict(ctx, c); err != nil {
		return err
	}
	now := time.Now().UTC()
	_ = s.Store.UpsertSyncFileState(ctx, &store.SyncFileState{
		ID: store.NewID(), JobID: j.ID, RelPath: rel, Status: store.SyncFileConflict,
		ConflictID: c.ID, SHA256: base.SHA256, Size: base.Size, Mtime: base.Mtime,
		FileID: base.FileID, Deleted: base.Deleted, LastSyncedAt: &now,
	})
	n, _ := s.Store.CountOpenSyncConflicts(ctx, j.ID)
	_ = s.Store.UpdateSyncJobConflictsOpen(ctx, j.ID, n)
	return nil
}

func (s *Service) transferOrCopy(ctx context.Context, j *store.SyncJob, fromDev, fromPath, toDev, toPath string) error {
	if fromDev == toDev {
		if _, err := s.Storage.Copy(ctx, j.UserID, fromDev, fromPath, toPath); err != nil {
			return fmt.Errorf("copy %s → %s: %w", fromPath, toPath, err)
		}
		return nil
	}
	res, err := s.Storage.TransferBetween(ctx, storage.TransferBetweenRequest{
		UserID: j.UserID, FromDeviceID: fromDev, FromPath: fromPath,
		ToDeviceID: toDev, ToPath: toPath,
	})
	if err != nil {
		return fmt.Errorf("transfer %s → %s: %w", fromPath, toPath, err)
	}
	_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, 0, 0, fromPath, res.Transfer.ID)
	// Re-read progress counters are updated by caller; just wait.
	j2, _ := s.Store.GetSyncJobByID(ctx, j.ID)
	if j2 != nil {
		_ = s.Store.UpdateSyncJobProgress(ctx, j.ID, j2.FilesDone, j2.BytesDone, fromPath, res.Transfer.ID)
	}
	return s.waitTransfer(ctx, j.UserID, res.Transfer.ID)
}

// ResolveConflict applies keep_a | keep_b | keep_both for an open conflict.
// After a successful resolution the parent sync job is re-run so both nodes
// converge; open conflicts are skipped until resolved (persistent state).
func (s *Service) ResolveConflict(ctx context.Context, userID, conflictID, resolution string) (*store.SyncConflict, error) {
	out, jobID, err := s.applyConflictResolution(ctx, userID, conflictID, resolution)
	if err != nil {
		return nil, err
	}
	s.rerunAfterResolve(ctx, userID, jobID)
	return out, nil
}

// BatchResolveConflictItem is one entry in a batch resolve request.
type BatchResolveConflictItem struct {
	ID         string
	Resolution string
}

// BatchResolveResult reports per-item outcomes.
type BatchResolveResult struct {
	Resolved []store.SyncConflict
	Errors   []string
}

// BatchResolveConflicts applies resolutions sequentially; re-runs each affected job once.
func (s *Service) BatchResolveConflicts(ctx context.Context, userID string, items []BatchResolveConflictItem) (*BatchResolveResult, error) {
	out := &BatchResolveResult{}
	jobs := map[string]struct{}{}
	for _, it := range items {
		c, jobID, err := s.applyConflictResolution(ctx, userID, it.ID, it.Resolution)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", it.ID, err))
			continue
		}
		out.Resolved = append(out.Resolved, *c)
		jobs[jobID] = struct{}{}
	}
	for jobID := range jobs {
		s.rerunAfterResolve(ctx, userID, jobID)
	}
	return out, nil
}

func (s *Service) rerunAfterResolve(ctx context.Context, userID, jobID string) {
	if _, err := s.Run(ctx, userID, jobID); err != nil && !errors.Is(err, ErrBusy) {
		return
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, _ = s.waitSettled(waitCtx, userID, jobID)
}

func (s *Service) applyConflictResolution(ctx context.Context, userID, conflictID, resolution string) (*store.SyncConflict, string, error) {
	c, err := s.Store.GetSyncConflict(ctx, userID, conflictID)
	if err != nil {
		return nil, "", err
	}
	if c.Status != store.SyncConflictOpen {
		return nil, "", fmt.Errorf("conflict is not open")
	}
	j, err := s.Store.GetSyncJob(ctx, userID, c.JobID)
	if err != nil {
		return nil, "", err
	}
	switch resolution {
	case store.SyncResolveKeepA:
		if err := s.resolveKeepSide(ctx, j, c, "A"); err != nil {
			return nil, "", err
		}
	case store.SyncResolveKeepB:
		if err := s.resolveKeepSide(ctx, j, c, "B"); err != nil {
			return nil, "", err
		}
	case store.SyncResolveKeepBoth:
		if err := s.resolveKeepBoth(ctx, j, c); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("resolution must be keep_a, keep_b, or keep_both")
	}
	if err := s.Store.ResolveSyncConflict(ctx, c.ID, resolution); err != nil {
		return nil, "", err
	}
	n, _ := s.Store.CountOpenSyncConflicts(ctx, j.ID)
	_ = s.Store.UpdateSyncJobConflictsOpen(ctx, j.ID, n)
	out, err := s.Store.GetSyncConflict(ctx, userID, conflictID)
	if err != nil {
		return nil, "", err
	}
	return out, j.ID, nil
}

func (s *Service) resolveKeepSide(ctx context.Context, j *store.SyncJob, c *store.SyncConflict, side string) error {
	var snap sideSnap
	var exists, deleted bool
	if side == "A" {
		exists, deleted = c.AExists, c.ADeleted
		snap = sideSnap{Exists: c.AExists, Size: c.ASize, Mtime: c.AMtime, SHA256: c.ASHA256}
	} else {
		exists, deleted = c.BExists, c.BDeleted
		snap = sideSnap{Exists: c.BExists, Size: c.BSize, Mtime: c.BMtime, SHA256: c.BSHA256}
	}
	var done, bytes int64
	if deleted || !exists {
		other := "B"
		if side == "B" {
			other = "A"
		}
		return s.applyDelete(ctx, j, other, c.RelPath, &done, &bytes)
	}
	from, to := side, "B"
	if side == "B" {
		from, to = "B", "A"
	}
	return s.pushFile(ctx, j, from, to, c.RelPath, snap, &done, &bytes)
}

func (s *Service) resolveKeepBoth(ctx context.Context, j *store.SyncJob, c *store.SyncConflict) error {
	var done, bytes int64
	// Preserve B first (before A overwrites the shared path).
	if c.BExists {
		bName := "B"
		if dev, err := s.Store.GetDeviceByID(ctx, j.DestDeviceID); err == nil && dev.Name != "" {
			bName = dev.Name
		}
		conflictRel := UniqueConflictCopyRelPath(c.RelPath, bName, c.BMtime, func(rel string) bool {
			aPath := joinRoot(j.SourcePath, rel)
			bPath := joinRoot(j.DestPath, rel)
			_, errA := s.Storage.Stat(ctx, j.UserID, j.SourceDeviceID, aPath)
			_, errB := s.Storage.Stat(ctx, j.UserID, j.DestDeviceID, bPath)
			return errA == nil || errB == nil
		})
		bPath := joinRoot(j.DestPath, c.RelPath)
		if _, err := s.Storage.Stat(ctx, j.UserID, j.DestDeviceID, bPath); err != nil {
			return fmt.Errorf("keep_both: B file missing at %s", bPath)
		}
		if err := s.transferOrCopy(ctx, j, j.DestDeviceID, bPath, j.SourceDeviceID, joinRoot(j.SourcePath, conflictRel)); err != nil {
			return err
		}
		if err := s.transferOrCopy(ctx, j, j.DestDeviceID, bPath, j.DestDeviceID, joinRoot(j.DestPath, conflictRel)); err != nil {
			return err
		}
		b := sideSnap{Exists: true, Size: c.BSize, Mtime: c.BMtime, SHA256: c.BSHA256}
		_ = s.markSynced(ctx, j.ID, conflictRel, b, false, &done, &bytes)
	}
	if c.AExists {
		a := sideSnap{Exists: true, Size: c.ASize, Mtime: c.AMtime, SHA256: c.ASHA256}
		if err := s.pushFile(ctx, j, "A", "B", c.RelPath, a, &done, &bytes); err != nil {
			return err
		}
	} else if c.ADeleted || !c.AExists {
		if err := s.applyDelete(ctx, j, "B", c.RelPath, &done, &bytes); err != nil {
			return err
		}
	}
	return nil
}
