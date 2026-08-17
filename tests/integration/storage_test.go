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

	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestStorageRoundTrip(t *testing.T) {
	ts, cl, _ := startCP(t, true) // force relay — deterministic; path still via Transfer
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "knot-storage")
	storeB := filepath.Join(shareB, "knot-storage")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(150 * time.Millisecond)

	// mkdir
	if _, err := cl.StorageMkdir(context.Background(), idB, "shared/inbox-tests"); err != nil {
		t.Fatal(err)
	}

	payload := []byte("storage round-trip payload")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "test.txt"), payload, 0o600)

	// upload VPS outbox → Home storage
	up, err := cl.StorageUpload(context.Background(), idB, "shared/test.txt", idA, "test.txt", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" {
		t.Fatalf("upload status=%s err=%s", done.Status, done.Error)
	}
	if done.Path != "relay" && done.Path != "direct" {
		t.Fatalf("unexpected transport path %q", done.Path)
	}

	got, err := os.ReadFile(filepath.Join(storeB, "shared", "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("home storage content mismatch")
	}

	// list
	ents, err := cl.StorageList(context.Background(), idB, "shared")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ents {
		if e.Path == "shared/test.txt" && !e.IsDir && e.Size == int64(len(payload)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("list missing file: %+v", ents)
	}

	// stat
	st, err := cl.StorageStat(context.Background(), idB, "shared/test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if st.Size != int64(len(payload)) || st.SHA256 != hexSum || st.IsDir {
		t.Fatalf("bad stat: %+v", st)
	}

	// download Home storage → VPS inbox
	rd, err := cl.StorageRead(context.Background(), idB, "shared/test.txt", idA)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	done2, err := cl.WaitTransfer(ctx2, rd.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done2.Status != "completed" {
		t.Fatalf("read status=%s err=%s", done2.Status, done2.Error)
	}
	down, err := os.ReadFile(filepath.Join(shareA, "inbox", "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(down, payload) {
		t.Fatalf("download content mismatch")
	}
	downSum := sha256.Sum256(down)
	if hex.EncodeToString(downSum[:]) != hexSum {
		t.Fatal("download sha mismatch")
	}

	// delete
	if err := cl.StorageDelete(context.Background(), idB, "shared/test.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(storeB, "shared", "test.txt")); !os.IsNotExist(err) {
		t.Fatalf("file still present after delete: %v", err)
	}
}

func TestStoragePathTraversalBlocked(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	share := t.TempDir()
	storeDir := filepath.Join(share, "knot-storage")
	id, stop := registerAndConnectStorage(t, ts, cl, "home", share, storeDir)
	defer stop()
	time.Sleep(100 * time.Millisecond)

	bads := []string{"../etc/passwd", "..\\Windows\\System32", "/etc/passwd", "shared/../../outside"}
	for _, p := range bads {
		if _, err := cl.StorageStat(context.Background(), id, p); err == nil {
			t.Fatalf("expected reject for %q", p)
		}
		if _, err := cl.StorageMkdir(context.Background(), id, p); err == nil {
			t.Fatalf("expected mkdir reject for %q", p)
		}
		if err := cl.StorageDelete(context.Background(), id, p); err == nil {
			t.Fatalf("expected delete reject for %q", p)
		}
	}
}

func TestStorageReadScopeCannotWrite(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	share := t.TempDir()
	storeDir := filepath.Join(share, "knot-storage")
	id, stop := registerAndConnectStorage(t, ts, cl, "home", share, storeDir)
	defer stop()
	time.Sleep(100 * time.Millisecond)

	_, tok, err := cl.CreateCredential(context.Background(), "ro-storage", []string{permissions.StorageRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tok)

	// read-scoped list should work
	if _, err := ro.StorageList(context.Background(), id, ""); err != nil {
		t.Fatalf("list should work with storage.read: %v", err)
	}
	// write ops denied
	_, err = ro.StorageMkdir(context.Background(), id, "shared/nope")
	if err == nil {
		t.Fatal("expected mkdir forbidden")
	}
	ae, ok := err.(*client.APIError)
	if !ok || ae.Code != apierrors.CodeForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
