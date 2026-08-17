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

func TestStoragePutAndContent(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	share := t.TempDir()
	storeDir := filepath.Join(share, "s")
	id, stop := registerAndConnectStorage(t, ts, cl, "home", share, storeDir)
	defer stop()
	time.Sleep(80 * time.Millisecond)

	payload := []byte("stage-5.2-browser-put-bytes")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	st, err := cl.StoragePut(context.Background(), id, "shared/put.txt", hexSum, int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.Path != "shared/put.txt" {
		t.Fatalf("%+v", st)
	}
	got, err := os.ReadFile(filepath.Join(storeDir, "shared", "put.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("disk mismatch")
	}

	// conflict
	_, err = cl.StoragePut(context.Background(), id, "shared/put.txt", hexSum, int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{})
	if err == nil {
		t.Fatal("expected name conflict")
	}
	if !client.IsCode(err, "name_conflict") {
		t.Fatalf("want name_conflict got %v", err)
	}

	st2, err := cl.StoragePut(context.Background(), id, "shared/put.txt", hexSum, int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{Conflict: "rename"})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Path == "shared/put.txt" {
		t.Fatalf("expected renamed path, got %s", st2.Path)
	}

	data, ctype, err := cl.StorageContent(context.Background(), id, "shared/put.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("content mismatch")
	}
	if ctype == "" {
		t.Fatal("missing content-type")
	}
}
