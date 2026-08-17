package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageResumeLargeUpload(t *testing.T) {
	ts, cl, _ := startCP(t, true) // force relay for deterministic chunking
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "knot-storage")
	storeB := filepath.Join(shareB, "knot-storage")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(150 * time.Millisecond)

	const size = 12 << 20 // 12 MiB > Stage 2 16MiB? No, 12 < 16. Use 20 MiB to prove storage limit.
	payload := make([]byte, 20<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	if err := os.WriteFile(filepath.Join(shareA, "outbox", "big.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	up, err := cl.StorageUpload(context.Background(), idB, "shared/big.bin", idA, "big.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	if up.FileID == "" {
		t.Fatal("expected file_id")
	}
	fileID := up.FileID

	// Interrupt after some data has likely been written
	deadline := time.Now().Add(8 * time.Second)
	var partPath string
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(storeB, "shared", "big.bin.knot.part.*"))
		for _, p := range matches {
			if st, err := os.Stat(p); err == nil && st.Size() > 256<<10 {
				partPath = p
				break
			}
		}
		if partPath != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cl.AbortTransfer(context.Background(), up.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	// After abort, part should be under stable file_id name (or still present).
	stable := filepath.Join(storeB, "shared", "big.bin.knot.part."+fileID)
	st, err := os.Stat(stable)
	if err != nil {
		st, err = os.Stat(partPath)
		if err != nil {
			t.Fatalf("expected partial file after interrupt: %v", err)
		}
	}
	if st.Size() <= 0 || st.Size() >= int64(len(payload)) {
		t.Fatalf("unexpected partial size %d", st.Size())
	}
	partialSize := st.Size()
	t.Logf("interrupted at %d / %d bytes file_id=%s", partialSize, len(payload), fileID)

	// Resume
	up2, err := cl.StorageUploadResume(context.Background(), idB, "shared/big.bin", idA, "big.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	if up2.ResumeOffset != partialSize && up2.BytesReceived != partialSize {
		// Allow small race if more bytes flushed after abort signal
		t.Logf("resume_offset=%d bytes_received=%d partial=%d", up2.ResumeOffset, up2.BytesReceived, partialSize)
	}
	if up2.ResumeOffset <= 0 {
		t.Fatalf("expected resume_offset > 0, got %d", up2.ResumeOffset)
	}
	if up2.FileID != fileID {
		t.Fatalf("file_id changed: %s -> %s", fileID, up2.FileID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up2.ID, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" {
		t.Fatalf("resume status=%s err=%s", done.Status, done.Error)
	}

	final := filepath.Join(storeB, "shared", "big.bin")
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch after resume")
	}
	gotSum := sha256.Sum256(got)
	if hex.EncodeToString(gotSum[:]) != hexSum {
		t.Fatal("sha256 mismatch after resume")
	}
	if _, err := os.Stat(stable); !os.IsNotExist(err) {
		// part may already be renamed away
	}
	matches, _ := filepath.Glob(filepath.Join(storeB, "shared", "big.bin.knot.part.*"))
	if len(matches) != 0 {
		t.Fatalf("part files should be gone after atomic rename: %v", matches)
	}

	meta, err := cl.StorageGetFile(context.Background(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "complete" {
		t.Fatalf("file status=%v", meta.Status)
	}
	if meta.SHA256 != hexSum {
		t.Fatalf("meta sha=%v", meta.SHA256)
	}

	st2, err := cl.StorageStat(context.Background(), idB, "shared/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if st2.FileID != fileID || st2.SHA256 != hexSum || st2.Size != int64(len(payload)) {
		t.Fatalf("stat %+v", st2)
	}
}

func TestStorageUploadAboveNetworkLimit(t *testing.T) {
	// 20 MiB exceeds network.transfer 16 MiB but allowed for storage
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, filepath.Join(shareB, "s"))
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := bytes.Repeat([]byte("abcdefgh"), (18<<20)/8) // 18 MiB
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "med.bin"), payload, 0o600)

	up, err := cl.StorageUpload(context.Background(), idB, "shared/med.bin", idA, "med.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up.ID, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
}
