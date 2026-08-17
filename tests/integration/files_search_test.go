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

func TestFilesSearchAcrossNodes(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "Home PC", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "VPS #3", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(120 * time.Millisecond)

	png := []byte("fake-png-logo")
	pdf := []byte("%PDF-1.4 logo")
	zip := []byte("PK\x03\x04 site-backup")
	putFile(t, cl, idA, "projects/site/logo.png", png)
	putFile(t, cl, idB, "var/www/site/logo.svg", []byte("<svg>logo</svg>"))
	putFile(t, cl, idA, "backups/site.zip", zip)
	putFile(t, cl, idB, "Documents/logo.pdf", pdf)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := cl.FilesReindex(ctx, ""); err != nil {
		t.Fatal(err)
	}

	hits, err := cl.FilesSearch(ctx, client.FileSearchQuery{Query: "logo"})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]client.FileHit{}
	devices := map[string]bool{}
	for _, h := range hits {
		byPath[h.DeviceID+":"+h.Path] = h
		devices[h.DeviceID] = true
		if h.DeviceName == "" {
			t.Fatalf("missing device_name for %s", h.Path)
		}
	}
	if !devices[idA] || !devices[idB] {
		t.Fatalf("search logo should span both nodes, got %+v", hits)
	}
	if _, ok := byPath[idA+":projects/site/logo.png"]; !ok {
		t.Fatalf("missing Home PC logo.png in %+v", hits)
	}
	if _, ok := byPath[idB+":var/www/site/logo.svg"]; !ok {
		t.Fatalf("missing VPS logo.svg in %+v", hits)
	}
	if _, ok := byPath[idB+":Documents/logo.pdf"]; !ok {
		t.Fatalf("missing logo.pdf in %+v", hits)
	}

	folderHits, err := cl.FilesSearch(ctx, client.FileSearchQuery{Folder: "projects"})
	if err != nil {
		t.Fatal(err)
	}
	if len(folderHits) != 1 || folderHits[0].Name != "site" || !folderHits[0].IsDirectory {
		t.Fatalf("browse projects: %+v", folderHits)
	}

	if err := cl.StorageDelete(ctx, idA, "projects/site/logo.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.FilesReindex(ctx, idA); err != nil {
		t.Fatal(err)
	}
	after, err := cl.FilesSearch(ctx, client.FileSearchQuery{Query: "logo.png"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range after {
		if h.DeviceID == idA && h.Path == "projects/site/logo.png" {
			t.Fatal("deleted file still in index")
		}
	}

	tr, err := cl.StorageTransfer(ctx, idA, "backups/site.zip", idB, "incoming/site.zip")
	if err != nil {
		t.Fatal(err)
	}
	done, err := cl.WaitTransfer(ctx, tr.ID, 50*time.Millisecond)
	if err != nil || done.Status != "completed" {
		t.Fatalf("send to node %+v %v", done, err)
	}
	got, err := os.ReadFile(filepath.Join(storeB, "incoming", "site.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, zip) {
		t.Fatal("transferred bytes mismatch")
	}
}

func putFile(t *testing.T, cl *client.Client, deviceID, path string, payload []byte) {
	t.Helper()
	sum := sha256.Sum256(payload)
	st, err := cl.StoragePut(context.Background(), deviceID, path, hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if st == nil {
		t.Fatal("empty stat")
	}
}
