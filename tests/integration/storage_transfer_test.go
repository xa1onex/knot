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
)

func TestStorageTransferBetweenNodes(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "home", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "vps", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := []byte("cross-node-payload-stage-5.1")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "x.bin"), payload, 0o600)

	// Seed on B from A's outbox (devices must differ for transfer create).
	up, err := cl.StorageUpload(context.Background(), idB, "shared/seed.bin", idA, "x.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if done, _ := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond); done.Status != "completed" {
		t.Fatalf("seed %+v", done)
	}

	tr, err := cl.StorageTransfer(context.Background(), idB, "shared/seed.bin", idA, "projects/seed.bin")
	if err != nil {
		t.Fatal(err)
	}
	if tr.ID == "" {
		t.Fatal("expected transfer id")
	}
	done, err := cl.WaitTransfer(ctx, tr.ID, 50*time.Millisecond)
	if err != nil || done.Status != "completed" {
		t.Fatalf("transfer %+v %v", done, err)
	}
	got, err := os.ReadFile(filepath.Join(storeA, "projects", "seed.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch after cross-node transfer")
	}
}
