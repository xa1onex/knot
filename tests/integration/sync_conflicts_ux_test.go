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

func TestConflictPersistsAcrossCPRestart(t *testing.T) {
	// Conflict is durable DB state — survives Control Plane restart (closing the app).
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knot.db")

	ts1, cl1, st1 := startCPWithDB(t, true, dbPath)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts1, cl1, "home", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts1, cl1, "mac", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	put := func(cl *client.Client, dev, path string, body []byte) {
		t.Helper()
		sum := sha256.Sum256(body)
		if _, err := cl.StoragePut(ctx, dev, path, hex.EncodeToString(sum[:]), int64(len(body)), bytes.NewReader(body), client.StoragePutOpts{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
	}

	job, err := cl1.CreateSyncJob(ctx, client.CreateSyncJobRequest{
		Mode: "two_way", SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	put(cl1, idA, "projects/config.json", []byte(`{"v":1}`))
	runWait(t, cl1, job.ID)

	put(cl1, idA, "projects/config.json", []byte(`{"v":"a"}`))
	put(cl1, idB, "projects/config.json", []byte(`{"v":"b"}`))
	runWait(t, cl1, job.ID)

	list1, err := cl1.ListSyncConflicts(ctx, job.ID)
	if err != nil || len(list1) == 0 || list1[0].Status != "open" {
		t.Fatalf("expected open conflict before restart: %v %+v", err, list1)
	}
	conflictID := list1[0].ID
	jobID := job.ID

	// Close CP (agents already have defer stop). Detach store for reopen.
	stopA()
	stopB()
	ts1.Close()
	_ = st1.Close()
	time.Sleep(50 * time.Millisecond)

	ts2, cl2, _ := startCPWithDB(t, true, dbPath)
	_ = ts2

	list2, err := cl2.ListSyncConflictsOpt(ctx, jobID, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range list2 {
		if c.ID == conflictID && c.Status == "open" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("conflict %s missing after CP restart; got %+v", conflictID, list2)
	}
}

func TestKeepBothNeverOverwritesExistingConflictCopy(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "Home PC", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "MacBook", shareB, storeB)
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
		Mode: "two_way", SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	put(idA, "projects/config.json", []byte("v0"))
	runWait(t, cl, job.ID)

	put(idA, "projects/config.json", []byte("version-A"))
	put(idB, "projects/config.json", []byte("version-B"))
	runWait(t, cl, job.ID)
	conflicts, err := cl.ListSyncConflicts(ctx, job.ID)
	if err != nil || len(conflicts) == 0 {
		t.Fatalf("conflicts: %v", err)
	}
	c := conflicts[0]
	suggested := c.KeepBothSuggestedName
	if suggested == "" {
		t.Fatal("expected keep_both_suggested_name enrichment")
	}
	pre := []byte("pre-existing-copy")
	prePathA := filepath.Join(storeA, "projects", filepath.Base(suggested))
	prePathB := filepath.Join(storeB, "projects", filepath.Base(suggested))
	if err := os.WriteFile(prePathA, pre, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prePathB, pre, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := cl.ResolveSyncConflict(ctx, c.ID, "keep_both"); err != nil {
		t.Fatal(err)
	}
	gotPre, _ := os.ReadFile(prePathA)
	if !bytes.Equal(gotPre, pre) {
		t.Fatalf("pre-existing conflict copy was overwritten: %q", gotPre)
	}
	matches, _ := filepath.Glob(filepath.Join(storeA, "projects", "config.conflict-*.json"))
	foundNew := false
	for _, m := range matches {
		if m == prePathA {
			continue
		}
		body, _ := os.ReadFile(m)
		if bytes.Equal(body, []byte("version-B")) {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Fatalf("expected bumped conflict copy with B content; files=%v", matches)
	}
}

func TestBatchResolveConflicts(t *testing.T) {
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
		Mode: "two_way", SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	put(idA, "projects/a.txt", []byte("0"))
	put(idA, "projects/b.txt", []byte("0"))
	runWait(t, cl, job.ID)
	put(idA, "projects/a.txt", []byte("A1"))
	put(idB, "projects/a.txt", []byte("B1"))
	put(idA, "projects/b.txt", []byte("A2"))
	put(idB, "projects/b.txt", []byte("B2"))
	runWait(t, cl, job.ID)
	list, err := cl.ListSyncConflicts(ctx, job.ID)
	if err != nil || len(list) < 2 {
		t.Fatalf("want >=2 conflicts, got %d err=%v", len(list), err)
	}
	ids := []string{list[0].ID, list[1].ID}
	resolved, errs, err := cl.BatchResolveSyncConflicts(ctx, ids, "keep_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved=%d", len(resolved))
	}
	for _, r := range resolved {
		if r.Status != "resolved" || r.Resolution != "keep_a" {
			t.Fatalf("%+v", r)
		}
	}
}
