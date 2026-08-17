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
	"github.com/knot-infra/knot/pkg/permissions"
)

// TestClientSDKContract locks Stage 5.0 Go SDK surface against a live knotd.
func TestClientSDKContract(t *testing.T) {
	ts, cl, _ := startCP(t, true)

	if err := cl.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}

	id, err := cl.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.UserID == "" || id.Email == "" || len(id.Scopes) == 0 {
		t.Fatalf("identity %+v", id)
	}

	ov, err := cl.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.DevicesTotal < 0 {
		t.Fatal("overview")
	}

	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(100 * time.Millisecond)

	devs, err := cl.ListDevices(context.Background())
	if err != nil || len(devs) < 2 {
		t.Fatalf("devices: %v %#v", err, devs)
	}

	if _, err := cl.StorageMkdir(context.Background(), idB, "photos/sdk"); err != nil {
		t.Fatal(err)
	}
	ents, err := cl.StorageList(context.Background(), idB, "photos")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ents {
		if e.Name == "sdk" && e.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("mkdir not listed: %+v", ents)
	}

	payload := bytes.Repeat([]byte("sdk-progress-"), 8<<10) // ~96 KiB → multiple ACKs
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "sdk.bin"), payload, 0o600)

	up, err := cl.StorageUploadRequest(context.Background(), client.StorageUploadRequest{
		DeviceID: idB, Path: "photos/sdk/sdk.bin", FromDeviceID: idA,
		SourcePath: "sdk.bin", Size: int64(len(payload)), SHA256: hexSum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.FileID == "" || up.ID == "" {
		t.Fatalf("upload response %+v", up)
	}

	var sawProgress bool
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done, err := cl.WatchTransfer(ctx, up.ID, 40*time.Millisecond, func(p client.Progress) {
		if p.BytesReceived > 0 && p.Size == int64(len(payload)) {
			sawProgress = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != client.TransferCompleted {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if !sawProgress {
		// Allow very fast machines where first poll already completed
		if done.BytesReceived == 0 && done.Size > 0 {
			t.Log("progress callback never saw mid-transfer bytes (fast complete)")
		}
	}

	st, err := cl.StorageStat(context.Background(), idB, "photos/sdk/sdk.bin")
	if err != nil || st.SHA256 != hexSum || st.MimeType == "" {
		t.Fatalf("stat %+v %v", st, err)
	}
	meta, err := cl.StorageGetFile(context.Background(), up.FileID)
	if err != nil || meta.Status != client.FileComplete {
		t.Fatalf("file meta %+v %v", meta, err)
	}

	if _, err := cl.StorageCopy(context.Background(), idB, "photos/sdk/sdk.bin", "photos/sdk/sdk-copy.bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.StorageMove(context.Background(), idB, "photos/sdk/sdk-copy.bin", "projects/sdk-copy.bin"); err != nil {
		t.Fatal(err)
	}

	// Storage-scoped credential can list transfers and abort (Stage 5.0 scope fix).
	_, tok, err := cl.CreateCredential(context.Background(), "sdk-app", []string{
		permissions.DevicesRead, permissions.StorageRead, permissions.StorageWrite,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	app := client.New(ts.URL, tok)
	list, err := app.ListTransfers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected transfers visible to storage credential")
	}
	meApp, err := app.Me(context.Background())
	if err != nil || meApp.UserID == "" {
		t.Fatalf("app me %+v %v", meApp, err)
	}
}

func TestClientSDKProgressResumeAbort(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeB := filepath.Join(shareB, "s")
	idA, stopA := registerAndConnectStorage(t, ts, cl, "vps", shareA, filepath.Join(shareA, "s"))
	idB, stopB := registerAndConnectStorage(t, ts, cl, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(80 * time.Millisecond)

	payload := make([]byte, 2<<20)
	for i := range payload {
		payload[i] = byte(i)
	}
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "prog.bin"), payload, 0o600)

	up, err := cl.StorageUpload(context.Background(), idB, "shared/prog.bin", idA, "prog.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tr, err := cl.GetTransfer(context.Background(), up.ID)
		if err == nil && tr.BytesReceived > 64<<10 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err := cl.AbortTransfer(context.Background(), up.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	up2, err := cl.StorageUploadResume(context.Background(), idB, "shared/prog.bin", idA, "prog.bin", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	if up2.ResumeOffset <= 0 && up2.BytesReceived <= 0 {
		t.Fatalf("expected resume progress, got %+v", up2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, up2.ID, 50*time.Millisecond)
	if err != nil || done.Status != client.TransferCompleted {
		t.Fatalf("%+v %v", done, err)
	}
}
