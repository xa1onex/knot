package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestDualUploadSamePathNoMix(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payloadA := bytes.Repeat([]byte("AAAA"), 64<<10) // 256 KiB
	payloadB := bytes.Repeat([]byte("BBBB"), 64<<10)
	sumA := hex.EncodeToString(sha256Digest(payloadA))
	sumB := hex.EncodeToString(sha256Digest(payloadB))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "a.bin"), payloadA, 0o600)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "b.bin"), payloadB, 0o600)

	var ua, ub *client.Transfer
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ua, errA = cl.StorageUpload(context.Background(), idB, "shared/x.bin", idA, "a.bin", int64(len(payloadA)), sumA)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		ub, errB = cl.StorageUpload(context.Background(), idB, "shared/x.bin", idA, "b.bin", int64(len(payloadB)), sumB)
	}()
	wg.Wait()
	if errA != nil {
		t.Fatal(errA)
	}
	if errB != nil {
		t.Fatal(errB)
	}
	if ua.FileID == "" || ub.FileID == "" || ua.FileID == ub.FileID {
		t.Fatalf("expected distinct file_ids, got %q and %q", ua.FileID, ub.FileID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	da, _ := cl.WaitTransfer(ctx, ua.ID, 50*time.Millisecond)
	db, _ := cl.WaitTransfer(ctx, ub.ID, 50*time.Millisecond)
	if da.Status != "completed" && db.Status != "completed" {
		t.Fatalf("neither completed: a=%s b=%s", da.Status, db.Status)
	}
	got, err := os.ReadFile(filepath.Join(storeB, "shared", "x.bin"))
	if err != nil {
		t.Fatal(err)
	}
	gotSum := hex.EncodeToString(sha256Digest(got))
	if gotSum != sumA && gotSum != sumB {
		t.Fatalf("mixed or corrupt content sha=%s", gotSum)
	}
	if !bytes.Equal(got, payloadA) && !bytes.Equal(got, payloadB) {
		t.Fatal("file content is neither A nor B (possible mix)")
	}
}

func TestUploadDeleteRace(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := bytes.Repeat([]byte("UD"), 32<<10)
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "ud.bin"), payload, 0o600)

	up, err := cl.StorageUpload(context.Background(), idB, "shared/ud.bin", idA, "ud.bin", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = cl.StorageDelete(context.Background(), idB, "shared/ud.bin")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, _ = cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond)
	if st, err := os.Stat(filepath.Join(storeB, "shared", "ud.bin")); err == nil {
		got, _ := os.ReadFile(filepath.Join(storeB, "shared", "ud.bin"))
		if st.Size() == int64(len(payload)) && hex.EncodeToString(sha256Digest(got)) != sum {
			t.Fatal("corrupt final file after upload+delete race")
		}
	}
}

func TestUploadDownloadRace(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := bytes.Repeat([]byte("UPDL"), 16<<10)
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "updl.bin"), payload, 0o600)

	up, err := cl.StorageUpload(context.Background(), idB, "shared/updl.bin", idA, "updl.bin", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond)
	if err != nil || done.Status != "completed" {
		t.Fatalf("upload: %+v %v", done, err)
	}

	rd, err := cl.StorageRead(context.Background(), idB, "shared/updl.bin", idA)
	if err != nil {
		t.Fatal(err)
	}
	done2, err := cl.WaitTransfer(ctx, rd.ID, 50*time.Millisecond)
	if err != nil || done2.Status != "completed" {
		t.Fatalf("download: %+v %v", done2, err)
	}
	got, err := os.ReadFile(filepath.Join(shareA, "inbox", "updl.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("download content mismatch")
	}
}

func TestWrongSHA256Rejects(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := []byte("correct-bytes-wrong-declared-sha")
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "badsha.bin"), payload, 0o600)
	fake := hex.EncodeToString(sha256Digest([]byte("not-the-payload")))

	up, err := cl.StorageUpload(context.Background(), idB, "shared/badsha.bin", idA, "badsha.bin", int64(len(payload)), fake)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status == "completed" {
		t.Fatal("expected failure on sha256 mismatch")
	}
	if _, err := os.Stat(filepath.Join(storeB, "shared", "badsha.bin")); err == nil {
		t.Fatal("final path must not exist after sha mismatch")
	}
}

func TestConcurrentMkdir(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	share := t.TempDir()
	id, stop := registerAndConnectStorage(t, ts, cl, "home", share, filepath.Join(share, "s"))
	defer stop()
	time.Sleep(80 * time.Millisecond)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cl.StorageMkdir(context.Background(), id, "shared/concurrent-dir")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("all mkdir failed")
	}
	st, err := cl.StorageStat(context.Background(), id, "shared/concurrent-dir")
	if err != nil || !st.IsDir {
		t.Fatalf("dir missing: %v %+v", err, st)
	}
}

func TestConcurrentRename(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := []byte("rename-race")
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "rn.txt"), payload, 0o600)
	up, err := cl.StorageUpload(context.Background(), idB, "shared/rn.txt", idA, "rn.txt", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if done, _ := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond); done.Status != "completed" {
		t.Fatalf("%+v", done)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := cl.StorageMove(context.Background(), idB, "shared/rn.txt", "projects/rn-a.txt")
		errs <- err
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		_, err := cl.StorageMove(context.Background(), idB, "shared/rn.txt", "projects/rn-b.txt")
		errs <- err
	}()
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("both renames failed")
	}
	// Exactly one destination should exist with full content.
	found := 0
	for _, name := range []string{"rn-a.txt", "rn-b.txt"} {
		p := filepath.Join(storeB, "projects", name)
		if got, err := os.ReadFile(p); err == nil && bytes.Equal(got, payload) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected exactly one valid destination, found %d", found)
	}
}

func TestCopyDuringUpload(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	// Seed a complete file, then start a large upload to another path and copy the seed.
	seed := []byte("seed-complete")
	sumSeed := hex.EncodeToString(sha256Digest(seed))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "seed.txt"), seed, 0o600)
	up0, err := cl.StorageUpload(context.Background(), idB, "shared/seed.txt", idA, "seed.txt", int64(len(seed)), sumSeed)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if done, _ := cl.WaitTransfer(ctx, up0.ID, 50*time.Millisecond); done.Status != "completed" {
		t.Fatalf("seed: %+v", done)
	}

	big := bytes.Repeat([]byte("BIG!"), 256<<10) // 1 MiB
	sumBig := hex.EncodeToString(sha256Digest(big))
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "big.bin"), big, 0o600)
	up, err := cl.StorageUpload(context.Background(), idB, "shared/uploading.bin", idA, "big.bin", int64(len(big)), sumBig)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := cl.StorageCopy(context.Background(), idB, "shared/seed.txt", "shared/seed-copy.txt"); err != nil {
		t.Fatal(err)
	}
	if done, _ := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond); done.Status != "completed" {
		t.Fatalf("upload: %+v", done)
	}
	got, err := os.ReadFile(filepath.Join(storeB, "shared", "seed-copy.txt"))
	if err != nil || !bytes.Equal(got, seed) {
		t.Fatalf("copy corrupted during concurrent upload: %v", err)
	}
}

func TestDeleteDuringResume(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i)
	}
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "delres.bin"), payload, 0o600)

	up, err := cl.StorageUpload(context.Background(), idB, "shared/delres.bin", idA, "delres.bin", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(storeB, "shared", "delres.bin.knot.part.*"))
		if len(matches) > 0 {
			if st, err := os.Stat(matches[0]); err == nil && st.Size() > 64<<10 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cl.AbortTransfer(context.Background(), up.ID)
	time.Sleep(300 * time.Millisecond)

	up2, err := cl.StorageUploadResume(context.Background(), idB, "shared/delres.bin", idA, "delres.bin", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = cl.StorageDelete(context.Background(), idB, "shared/delres.bin")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, _ = cl.WaitTransfer(ctx, up2.ID, 50*time.Millisecond)
	// After settle: either absent or a full valid file — never a mixed/corrupt final.
	if st, err := os.Stat(filepath.Join(storeB, "shared", "delres.bin")); err == nil {
		got, _ := os.ReadFile(filepath.Join(storeB, "shared", "delres.bin"))
		if st.Size() == int64(len(payload)) && hex.EncodeToString(sha256Digest(got)) != sum {
			t.Fatal("corrupt final after delete-during-resume")
		}
	}
}

func TestMoveAndCopy(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	payload := []byte("move-copy-payload")
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "mc.txt"), payload, 0o600)
	up, err := cl.StorageUpload(context.Background(), idB, "shared/mc.txt", idA, "mc.txt", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if done, _ := cl.WaitTransfer(ctx, up.ID, 50*time.Millisecond); done.Status != "completed" {
		t.Fatalf("%+v", done)
	}

	if _, err := cl.StorageCopy(context.Background(), idB, "shared/mc.txt", "shared/mc-copy.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.StorageMove(context.Background(), idB, "shared/mc-copy.txt", "projects/mc-copy.txt"); err != nil {
		t.Fatal(err)
	}
	st, err := cl.StorageStat(context.Background(), idB, "projects/mc-copy.txt")
	if err != nil {
		t.Fatal(err)
	}
	if st.SHA256 != sum || st.MimeType == "" || st.Name != "mc-copy.txt" {
		t.Fatalf("stat %+v", st)
	}
}

func TestQuotaMaxFile(t *testing.T) {
	ts, cl, st := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, filepath.Join(shareB, "s"))
	defer stopA()
	defer stopB()
	time.Sleep(80 * time.Millisecond)

	cid, tok, err := cl.CreateCredential(context.Background(), "quota", []string{
		permissions.StorageRead, permissions.StorageWrite, permissions.DevicesRead,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByEmail(context.Background(), "admin@node.local")
	if err != nil {
		t.Fatal(err)
	}
	max := int64(100)
	if err := st.SetCredentialQuotas(context.Background(), u.ID, cid, nil, &max, nil); err != nil {
		t.Fatal(err)
	}
	qcl := client.New(ts.URL, tok)
	payload := bytes.Repeat([]byte("q"), 200)
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "q.bin"), payload, 0o600)
	_, err = qcl.StorageUpload(context.Background(), idB, "shared/q.bin", idA, "q.bin", int64(len(payload)), sum)
	if err == nil {
		t.Fatal("expected quota error")
	}
	ae, ok := err.(*client.APIError)
	if !ok || ae.Code != "quota_exceeded" {
		t.Fatalf("expected quota_exceeded, got %#v (%v)", err, err)
	}
}

func TestQuotaExceededOnResume(t *testing.T) {
	ts, cl, st := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(80 * time.Millisecond)

	cid, tok, err := cl.CreateCredential(context.Background(), "quota-resume", []string{
		permissions.StorageRead, permissions.StorageWrite, permissions.DevicesRead,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByEmail(context.Background(), "admin@node.local")
	if err != nil {
		t.Fatal(err)
	}
	// Large enough to start a partial upload, then tighten max file for resume.
	maxStart := int64(8 << 20)
	if err := st.SetCredentialQuotas(context.Background(), u.ID, cid, nil, &maxStart, nil); err != nil {
		t.Fatal(err)
	}
	qcl := client.New(ts.URL, tok)

	payload := make([]byte, 2<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	sum := hex.EncodeToString(sha256Digest(payload))
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "qr.bin"), payload, 0o600)

	up, err := qcl.StorageUpload(context.Background(), idB, "shared/qr.bin", idA, "qr.bin", int64(len(payload)), sum)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(storeB, "shared", "qr.bin.knot.part.*"))
		if len(matches) > 0 {
			if stInfo, err := os.Stat(matches[0]); err == nil && stInfo.Size() > 64<<10 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cl.AbortTransfer(context.Background(), up.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	tiny := int64(100)
	if err := st.SetCredentialQuotas(context.Background(), u.ID, cid, nil, &tiny, nil); err != nil {
		t.Fatal(err)
	}
	_, err = qcl.StorageUploadResume(context.Background(), idB, "shared/qr.bin", idA, "qr.bin", int64(len(payload)), sum)
	if err == nil {
		t.Fatal("expected quota exceeded on resume")
	}
}

func sha256Digest(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
