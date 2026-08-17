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

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestMCPExternalClientFlow(t *testing.T) {
	ts, admin, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "knot-storage")
	storeB := filepath.Join(shareB, "knot-storage")
	idA, stopA := registerAndConnectStorage(t, ts, admin, "vps", shareA, storeA)
	idB, stopB := registerAndConnectStorage(t, ts, admin, "home", shareB, storeB)
	defer stopA()
	defer stopB()
	time.Sleep(150 * time.Millisecond)

	// External client credential — same scopes CLI would use
	_, tok, err := admin.CreateCredential(context.Background(), "mcp-ext", []string{
		permissions.DevicesRead, permissions.StorageRead, permissions.StorageWrite,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ext := client.New(ts.URL, tok)
	srv := &mcp.Server{Client: ext, WaitTimeout: 20 * time.Second}
	ctx := context.Background()

	// 1. list devices
	devOut, err := srv.Call(ctx, mcp.ToolDevicesList, nil)
	if err != nil {
		t.Fatal(err)
	}
	devs := devOut.(map[string]any)["devices"].([]client.Device)
	if len(devs) < 2 {
		t.Fatalf("expected >=2 devices, got %d", len(devs))
	}

	// seed test.txt on home storage via upload from vps
	payload1 := []byte("hello from external client read")
	sum1 := sha256.Sum256(payload1)
	hex1 := hex.EncodeToString(sum1[:])
	_ = os.MkdirAll(filepath.Join(shareA, "outbox"), 0o700)
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "test.txt"), payload1, 0o600)

	_, err = srv.Call(ctx, mcp.ToolStorageUpload, map[string]any{
		"device_id": idB, "path": "shared/test.txt",
		"from_device_id": idA, "source_path": "test.txt",
		"size": float64(len(payload1)), "sha256": hex1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. list Home storage
	listOut, err := srv.Call(ctx, mcp.ToolStorageList, map[string]any{
		"device_id": idB, "path": "shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	ents := listOut.(map[string]any)["entries"].([]client.StorageEntry)
	found := false
	for _, e := range ents {
		if e.Path == "shared/test.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list missing test.txt: %+v", ents)
	}

	// 3. read (stat) test.txt — SHA-256
	stOut, err := srv.Call(ctx, mcp.ToolStorageStat, map[string]any{
		"device_id": idB, "path": "shared/test.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	st := stOut.(*client.StorageStat)
	if st.SHA256 != hex1 || st.Size != int64(len(payload1)) {
		t.Fatalf("stat mismatch: %+v", st)
	}

	// 4. upload test2.txt
	payload2 := []byte("upload via mcp test2")
	sum2 := sha256.Sum256(payload2)
	hex2 := hex.EncodeToString(sum2[:])
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "test2.txt"), payload2, 0o600)
	upOut, err := srv.Call(ctx, mcp.ToolStorageUpload, map[string]any{
		"device_id": idB, "path": "shared/test2.txt",
		"from_device_id": idA, "source_path": "test2.txt",
		"size": float64(len(payload2)), "sha256": hex2,
	})
	if err != nil {
		t.Fatal(err)
	}
	up := upOut.(*client.Transfer)
	if up.Status != "completed" || up.SHA256 != hex2 {
		t.Fatalf("upload transfer: %+v", up)
	}

	// 5. download test2.txt back to vps
	dlOut, err := srv.Call(ctx, mcp.ToolStorageDownload, map[string]any{
		"device_id": idB, "path": "shared/test2.txt", "to_device_id": idA,
	})
	if err != nil {
		t.Fatal(err)
	}
	dl := dlOut.(*client.Transfer)
	if dl.Status != "completed" {
		t.Fatalf("download: %+v", dl)
	}
	got, err := os.ReadFile(filepath.Join(shareA, "inbox", "test2.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload2) {
		t.Fatal("download content mismatch")
	}
	gotSum := sha256.Sum256(got)
	if hex.EncodeToString(gotSum[:]) != hex2 {
		t.Fatal("sha256 mismatch after download")
	}

	// storage.read alias same path
	rdOut, err := srv.Call(ctx, mcp.ToolStorageRead, map[string]any{
		"device_id": idB, "path": "shared/test.txt", "to_device_id": idA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rdOut.(*client.Transfer).Status != "completed" {
		t.Fatalf("storage.read failed: %+v", rdOut)
	}
}

func TestMCPInsufficientScope403(t *testing.T) {
	ts, admin, _ := startCP(t, true)
	share := t.TempDir()
	storeDir := filepath.Join(share, "knot-storage")
	id, stop := registerAndConnectStorage(t, ts, admin, "home", share, storeDir)
	defer stop()
	time.Sleep(100 * time.Millisecond)

	_, tok, err := admin.CreateCredential(context.Background(), "mcp-ro", []string{permissions.DevicesRead, permissions.StorageRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	srv := &mcp.Server{Client: client.New(ts.URL, tok)}

	_, err = srv.Call(context.Background(), mcp.ToolDevicesList, nil)
	if err != nil {
		t.Fatalf("devices.list should work: %v", err)
	}
	_, err = srv.Call(context.Background(), mcp.ToolStorageUpload, map[string]any{
		"device_id": id, "path": "shared/x.txt",
		"from_device_id": id, "source_path": "x",
		"size": float64(1), "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err == nil {
		t.Fatal("expected upload forbidden without storage.write")
	}
	ae, ok := err.(*client.APIError)
	if !ok || ae.Code != apierrors.CodeForbidden || ae.Status != 403 {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
}

func TestMCPRevokedCredential401(t *testing.T) {
	ts, admin, _ := startCP(t, true)
	cid, tok, err := admin.CreateCredential(context.Background(), "mcp-temp", []string{permissions.DevicesRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	srv := &mcp.Server{Client: client.New(ts.URL, tok)}
	if _, err := srv.Call(context.Background(), mcp.ToolDevicesList, nil); err != nil {
		t.Fatal(err)
	}
	if err := admin.RevokeCredential(context.Background(), cid); err != nil {
		t.Fatal(err)
	}
	_, err = srv.Call(context.Background(), mcp.ToolDevicesList, nil)
	if err == nil {
		t.Fatal("expected unauthorized after revoke")
	}
	ae, ok := err.(*client.APIError)
	if !ok || ae.Status != 401 {
		t.Fatalf("expected 401, got %v", err)
	}
	if ae.Code != apierrors.CodeUnauthorized && ae.Code != apierrors.CodeTokenRevoked {
		t.Fatalf("unexpected code %q", ae.Code)
	}
}
