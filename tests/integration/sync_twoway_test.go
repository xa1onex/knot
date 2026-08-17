package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

func TestTwoWaySyncBothDirections(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "home", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "mac", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	put := func(dev, path string, body []byte) {
		t.Helper()
		sum := sha256.Sum256(body)
		if _, err := cl.StoragePut(ctx, dev, path, hex.EncodeToString(sum[:]), int64(len(body)), bytes.NewReader(body), client.StoragePutOpts{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
	}

	job, err := cl.CreateSyncJob(ctx, client.CreateSyncJobRequest{
		Name: "tw projects", Mode: "two_way",
		SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A create → B
	put(idA, "projects/a-only.txt", []byte("from-a"))
	runWait(t, cl, job.ID)
	got, err := os.ReadFile(filepath.Join(storeB, "projects", "a-only.txt"))
	if err != nil || !bytes.Equal(got, []byte("from-a")) {
		t.Fatalf("A→B create: %v %q", err, got)
	}

	// B create → A
	put(idB, "projects/b-only.txt", []byte("from-b"))
	runWait(t, cl, job.ID)
	got, err = os.ReadFile(filepath.Join(storeA, "projects", "b-only.txt"))
	if err != nil || !bytes.Equal(got, []byte("from-b")) {
		t.Fatalf("B→A create: %v %q", err, got)
	}

	// A modify → B
	put(idA, "projects/a-only.txt", []byte("from-a-v2"))
	runWait(t, cl, job.ID)
	got, _ = os.ReadFile(filepath.Join(storeB, "projects", "a-only.txt"))
	if !bytes.Equal(got, []byte("from-a-v2")) {
		t.Fatalf("A modify → B got %q", got)
	}

	// B modify → A
	put(idB, "projects/b-only.txt", []byte("from-b-v2"))
	runWait(t, cl, job.ID)
	got, _ = os.ReadFile(filepath.Join(storeA, "projects", "b-only.txt"))
	if !bytes.Equal(got, []byte("from-b-v2")) {
		t.Fatalf("B modify → A got %q", got)
	}

	// A delete → B
	if err := cl.StorageDelete(ctx, idA, "projects/a-only.txt"); err != nil {
		t.Fatal(err)
	}
	runWait(t, cl, job.ID)
	if _, err := os.Stat(filepath.Join(storeB, "projects", "a-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected A delete mirrored on B, err=%v", err)
	}

	// B delete → A
	if err := cl.StorageDelete(ctx, idB, "projects/b-only.txt"); err != nil {
		t.Fatal(err)
	}
	runWait(t, cl, job.ID)
	if _, err := os.Stat(filepath.Join(storeA, "projects", "b-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected B delete mirrored on A, err=%v", err)
	}
}

func TestTwoWaySyncConflictKeepBoth(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "home", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "mac", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	body := []byte("shared-v1")
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	if _, err := cl.StoragePut(ctx, idA, "projects/file.txt", hexSum, int64(len(body)), bytes.NewReader(body), client.StoragePutOpts{}); err != nil {
		t.Fatal(err)
	}

	job, err := cl.CreateSyncJob(ctx, client.CreateSyncJobRequest{
		Mode: "two_way", SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	runWait(t, cl, job.ID)

	// Concurrent modify — different sizes so mtime/size candidate check cannot
	// falsely treat both sides as unchanged if clock resolution collapses.
	a2 := []byte("version-A-longer")
	b2 := []byte("version-B")
	sa := sha256.Sum256(a2)
	sb := sha256.Sum256(b2)
	if _, err := cl.StoragePut(ctx, idA, "projects/file.txt", hex.EncodeToString(sa[:]), int64(len(a2)), bytes.NewReader(a2), client.StoragePutOpts{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.StoragePut(ctx, idB, "projects/file.txt", hex.EncodeToString(sb[:]), int64(len(b2)), bytes.NewReader(b2), client.StoragePutOpts{Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := cl.RunSyncJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	done, err := cl.WaitSyncJob(wctx, job.ID, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed_with_conflicts" && done.ConflictsOpen < 1 {
		t.Fatalf("expected conflicts, got %+v", done)
	}
	conflicts, err := cl.ListSyncConflicts(ctx, job.ID)
	if err != nil || len(conflicts) < 1 {
		t.Fatalf("conflicts: %v %+v", err, conflicts)
	}
	c := conflicts[0]
	if c.Status != "open" {
		t.Fatalf("%+v", c)
	}

	// Neither side silently lost: both still have their versions before resolve
	ga, _ := os.ReadFile(filepath.Join(storeA, "projects", "file.txt"))
	gb, _ := os.ReadFile(filepath.Join(storeB, "projects", "file.txt"))
	if !bytes.Equal(ga, a2) || !bytes.Equal(gb, b2) {
		t.Fatalf("silent overwrite before resolve A=%q B=%q", ga, gb)
	}

	resolved, err := cl.ResolveSyncConflict(ctx, c.ID, "keep_both")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != "resolved" || resolved.Resolution != "keep_both" {
		t.Fatalf("%+v", resolved)
	}

	// A version at path on both; B preserved under deterministic conflict copy name.
	ga, _ = os.ReadFile(filepath.Join(storeA, "projects", "file.txt"))
	gb, _ = os.ReadFile(filepath.Join(storeB, "projects", "file.txt"))
	if !bytes.Equal(ga, a2) || !bytes.Equal(gb, a2) {
		t.Fatalf("after keep_both path should be A: A=%q B=%q", ga, gb)
	}
	matches, _ := filepath.Glob(filepath.Join(storeA, "projects", "file.conflict-*.txt"))
	if len(matches) == 0 {
		t.Fatal("expected conflict copy on A")
	}
	cbA, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	matchesB, _ := filepath.Glob(filepath.Join(storeB, "projects", "file.conflict-*.txt"))
	if len(matchesB) == 0 {
		t.Fatal("expected conflict copy on B")
	}
	cbB, err := os.ReadFile(matchesB[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cbA, b2) || !bytes.Equal(cbB, b2) {
		t.Fatalf("conflict copy should be B version: %q %q", cbA, cbB)
	}
	if resolved.KeepBothSuggestedName == "" {
		// optional enrichment — may be present after resolve
	}
}

func runWait(t *testing.T, cl *client.Client, jobID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := cl.RunSyncJob(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	done, err := cl.WaitSyncJob(ctx, jobID, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" && done.Status != "completed_with_conflicts" {
		t.Fatalf("sync status=%s err=%q", done.Status, done.LastError)
	}
}
