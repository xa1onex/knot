package offline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueCrashSafeAndRestart(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "q.db")
	ctx := context.Background()

	q, err := Open(Config{DBPath: db, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Append(ctx, OpCreate, "projects/a.txt", "fid-a", FileState{Deleted: true}, FileState{Path: "projects/a.txt", Size: 3, SHA256: "abc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Append(ctx, OpModify, "projects/b.txt", "fid-b", FileState{SHA256: "1"}, FileState{SHA256: "2"}); err != nil {
		t.Fatal(err)
	}
	_ = q.Close()

	q2, err := Open(Config{DBPath: db})
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	n, err := q2.CountPending(ctx)
	if err != nil || n != 2 {
		t.Fatalf("pending after restart: n=%d err=%v", n, err)
	}
	pending, err := q2.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{pending[0].ID, pending[1].ID}
	if err := q2.MarkSyncing(ctx, ids); err != nil {
		t.Fatal(err)
	}
	if err := q2.FinishFlush(ctx, map[string]struct{}{"projects/b.txt": {}}); err != nil {
		t.Fatal(err)
	}
	done, _ := q2.ListByStatus(ctx, StatusDone)
	conflict, _ := q2.ListByStatus(ctx, StatusConflict)
	if len(done) != 1 || len(conflict) != 1 {
		t.Fatalf("done=%d conflict=%d", len(done), len(conflict))
	}
}

func TestQueueDiskLimit(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	q, err := Open(Config{DBPath: filepath.Join(dir, "q.db"), MaxBytes: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	big := FileState{Path: "x", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if _, err := q.Append(ctx, OpCreate, "a", "1", FileState{}, big); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Append(ctx, OpCreate, "b", "2", FileState{}, big); err != ErrDiskLimit {
		t.Fatalf("want ErrDiskLimit, got %v", err)
	}
}

func TestScannerDetectsOfflineEdits(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	ctx := context.Background()
	_ = os.MkdirAll(filepath.Join(root, "projects"), 0o700)
	q, err := Open(Config{DBPath: filepath.Join(data, "q.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	sc := NewScanner(root, q)
	if err := sc.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "new.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "edit.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// second seed-like: absorb creates into baseline by scanning again after "online" seed
	// reopen: simulate restart then more edits
	_ = q.Close()
	q2, err := Open(Config{DBPath: filepath.Join(data, "q.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	n, _ := q2.CountPending(ctx)
	if n < 2 {
		t.Fatalf("expected create entries, got %d", n)
	}
	sc2 := NewScanner(root, q2)
	// clear pending by finishing as if synced, then seed
	pending, _ := q2.ListPending(ctx)
	ids := make([]string, len(pending))
	for i, e := range pending {
		ids[i] = e.ID
	}
	_ = q2.MarkSyncing(ctx, ids)
	_ = q2.FinishFlush(ctx, nil)
	_ = sc2.SeedBaseline(ctx)

	if err := os.WriteFile(filepath.Join(root, "projects", "edit.txt"), []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "projects", "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "renamed.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	// recreate new then delete is messy — just modify + create
	added, err := sc2.ScanOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if added < 1 {
		t.Fatalf("expected changes, added=%d", added)
	}
}

func TestNextBackoff(t *testing.T) {
	if NextBackoff(0) != time.Second {
		t.Fatal()
	}
	if NextBackoff(time.Second) != 2*time.Second {
		t.Fatal()
	}
	if NextBackoff(20*time.Second) != BackoffCap {
		t.Fatalf("got %v", NextBackoff(20*time.Second))
	}
}
