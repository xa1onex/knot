package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

func TestOneWaySyncInitialAndIncremental(t *testing.T) {
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
	const n = 20
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("f/%03d.txt", i)
		payload := []byte(fmt.Sprintf("payload-%03d-v1", i))
		sum := sha256.Sum256(payload)
		if _, err := cl.StoragePut(ctx, idA, "projects/"+name, hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{}); err != nil {
			t.Fatal(err)
		}
	}

	job, err := cl.CreateSyncJob(ctx, client.CreateSyncJobRequest{
		Name:           "projects home→mac",
		Mode:           "one_way",
		SourceDeviceID: idA,
		SourcePath:     "projects",
		DestDeviceID:   idB,
		DestPath:       "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.RunSyncJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	done, err := cl.WaitSyncJob(wctx, job.ID, 80*time.Millisecond)
	if err != nil || done.Status != "completed" {
		t.Fatalf("initial sync %+v %v", done, err)
	}
	if done.FilesDone < n {
		t.Fatalf("expected >=%d files synced, got %d (err=%q)", n, done.FilesDone, done.LastError)
	}

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("f/%03d.txt", i)
		got, err := os.ReadFile(filepath.Join(storeB, "projects", name))
		if err != nil {
			t.Fatalf("missing on B: %s: %v", name, err)
		}
		want := []byte(fmt.Sprintf("payload-%03d-v1", i))
		if !bytes.Equal(got, want) {
			t.Fatalf("content mismatch %s", name)
		}
	}

	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("f/%03d.txt", i)
		payload := []byte(fmt.Sprintf("payload-%03d-v2", i))
		sum := sha256.Sum256(payload)
		if _, err := cl.StoragePut(ctx, idA, "projects/"+name, hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := cl.RunSyncJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	wctx2, cancel2 := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel2()
	done2, err := cl.WaitSyncJob(wctx2, job.ID, 80*time.Millisecond)
	if err != nil || done2.Status != "completed" {
		t.Fatalf("incremental sync %+v %v", done2, err)
	}

	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("f/%03d.txt", i)
		got, err := os.ReadFile(filepath.Join(storeB, "projects", name))
		if err != nil {
			t.Fatal(err)
		}
		want := []byte(fmt.Sprintf("payload-%03d-v2", i))
		if !bytes.Equal(got, want) {
			t.Fatalf("incremental mismatch %s got %q", name, got)
		}
	}
	for i := 7; i < n; i++ {
		name := fmt.Sprintf("f/%03d.txt", i)
		got, _ := os.ReadFile(filepath.Join(storeB, "projects", name))
		want := []byte(fmt.Sprintf("payload-%03d-v1", i))
		if !bytes.Equal(got, want) {
			t.Fatalf("unchanged file mutated %s", name)
		}
	}
}

func TestOneWaySyncDeleteMirror(t *testing.T) {
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
	payload := []byte("to-delete")
	sum := sha256.Sum256(payload)
	if _, err := cl.StoragePut(ctx, idA, "projects/x.txt", hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{}); err != nil {
		t.Fatal(err)
	}

	job, err := cl.CreateSyncJob(ctx, client.CreateSyncJobRequest{
		SourceDeviceID: idA, SourcePath: "projects",
		DestDeviceID: idB, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, _ = cl.RunSyncJob(ctx, job.ID)
	if d, err := cl.WaitSyncJob(wctx, job.ID, 80*time.Millisecond); err != nil || d.Status != "completed" {
		t.Fatalf("sync1 %+v %v", d, err)
	}
	if _, err := os.Stat(filepath.Join(storeB, "projects", "x.txt")); err != nil {
		t.Fatal(err)
	}

	if err := cl.StorageDelete(ctx, idA, "projects/x.txt"); err != nil {
		t.Fatal(err)
	}
	_, _ = cl.RunSyncJob(ctx, job.ID)
	if d, err := cl.WaitSyncJob(wctx, job.ID, 80*time.Millisecond); err != nil || d.Status != "completed" {
		t.Fatalf("sync2 %+v %v", d, err)
	}
	if _, err := os.Stat(filepath.Join(storeB, "projects", "x.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected delete mirrored, err=%v", err)
	}
}
