package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/agent/offline"
	"github.com/knot-infra/knot/internal/agent/storfs"
	"github.com/knot-infra/knot/internal/agent/xfer"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/protocol"
)

type liveAgent struct {
	DeviceID string
	Token    string
	Pub      ed25519.PublicKey
	Priv     ed25519.PrivateKey
	stop     func()
}

func registerLiveAgent(t *testing.T, tsURL string, cl *client.Client, name, shareDir, storageDir string) *liveAgent {
	t.Helper()
	reg, err := cl.CreateRegToken(context.Background(), name, 1)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(protocol.RegisterRequest{
		RegistrationToken: reg, PublicKey: base64.RawURLEncoding.EncodeToString(pub),
		Name: name, Hostname: name, OS: "test", Arch: "amd64",
	})
	resp, err := http.Post(tsURL+"/v1/agent/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var regOut protocol.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&regOut)
	a := &liveAgent{DeviceID: regOut.DeviceID, Token: regOut.DeviceToken, Pub: pub, Priv: priv}
	a.stop = connectAgentWS(t, tsURL, a, shareDir, storageDir)
	return a
}

func connectAgentWS(t *testing.T, tsURL string, a *liveAgent, shareDir, storageDir string) func() {
	t.Helper()
	wsURL := "ws" + tsURL[len("http"):] + "/v1/agent/connect?token=" + a.Token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var challenge protocol.ChallengeMessage
	_ = json.Unmarshal(data, &challenge)
	sig := ed25519.Sign(a.Priv, []byte(challenge.Nonce))
	_ = conn.WriteJSON(protocol.ChallengeResponse{
		Type: protocol.TypeChallengeR, Nonce: challenge.Nonce,
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	})
	_, _, _ = conn.ReadMessage() // session
	_, _, _ = conn.ReadMessage() // welcome

	writeMu := &sync.Mutex{}
	xf, err := xfer.NewManager(a.DeviceID, shareDir, storageDir, a.Pub, a.Priv, conn, writeMu)
	if err != nil {
		t.Fatal(err)
	}
	sf, err := storfs.New(storageDir, func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &env) != nil {
				continue
			}
			switch env.Type {
			case protocol.TypeTransferOffer, protocol.TypeTransferStart, protocol.TypeTransferChunk,
				protocol.TypeTransferAck, protocol.TypeTransferComplete, protocol.TypeTransferAbort,
				protocol.TypePathNegotiate, protocol.TypePathCandidate, protocol.TypePathSelected:
				xf.Handle(msg)
			case protocol.TypeStorageOp:
				sf.Handle(msg)
			}
		}
	}()
	_ = conn.WriteJSON(protocol.Heartbeat{Type: protocol.TypeHeartbeat, Telemetry: protocol.Telemetry{Hostname: "t", OS: "test", Arch: "amd64"}})
	return func() {
		_ = conn.Close()
		<-done
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOfflineQueueSurvivesRestartAndFlush(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	agentA := registerLiveAgent(t, ts.URL, cl, "home", shareA, storeA)
	agentB := registerLiveAgent(t, ts.URL, cl, "mac", shareB, storeB)
	defer agentB.stop()
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
		Name: "offline tw", Mode: "two_way",
		SourceDeviceID: agentA.DeviceID, SourcePath: "projects",
		DestDeviceID: agentB.DeviceID, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	put(agentA.DeviceID, "projects/base.txt", []byte("base"))
	runWait(t, cl, job.ID)

	agentData := t.TempDir()
	q, err := offline.Open(offline.Config{DBPath: offline.DefaultDBPath(agentData)})
	if err != nil {
		t.Fatal(err)
	}
	sc := offline.NewScanner(storeA, q)
	if err := sc.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}

	agentA.stop() // offline
	time.Sleep(80 * time.Millisecond)

	projects := filepath.Join(storeA, "projects")
	_ = os.MkdirAll(projects, 0o700)
	if err := os.WriteFile(filepath.Join(projects, "new1.txt"), []byte("n1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "to-rename.txt"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "base.txt"), []byte("base-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "deleteme.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(projects, "deleteme.txt"))
	_ = os.Rename(filepath.Join(projects, "to-rename.txt"), filepath.Join(projects, "renamed.txt"))
	if err := os.WriteFile(filepath.Join(projects, "new3.txt"), []byte("n3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	n, err := q.CountPending(ctx)
	if err != nil || n < 5 {
		t.Fatalf("expected >=5 pending, got %d err=%v", n, err)
	}
	_ = q.Close()

	// Agent restart — queue must survive.
	q2, err := offline.Open(offline.Config{DBPath: offline.DefaultDBPath(agentData)})
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	n2, _ := q2.CountPending(ctx)
	if n2 < 5 {
		t.Fatalf("after restart pending=%d", n2)
	}

	// Network restored — reconnect same device and flush.
	agentA.stop = connectAgentWS(t, ts.URL, agentA, shareA, storeA)
	defer agentA.stop()
	time.Sleep(100 * time.Millisecond)

	pending, _ := q2.ListPending(ctx)
	ids := make([]string, len(pending))
	for i, e := range pending {
		ids[i] = e.ID
	}
	_ = q2.MarkSyncing(ctx, ids)

	flush, err := cl.FlushSync(ctx, agentA.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(flush.JobIDs) == 0 {
		t.Fatalf("flush jobs empty: %+v", flush)
	}
	conflict := map[string]struct{}{}
	for _, p := range flush.ConflictPaths {
		conflict[p] = struct{}{}
	}
	if err := q2.FinishFlush(ctx, conflict); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(storeB, "projects", "base.txt"))
	if err != nil || !bytes.Equal(got, []byte("base-v2")) {
		t.Fatalf("base sync: %v %q", err, got)
	}
	got, err = os.ReadFile(filepath.Join(storeB, "projects", "new1.txt"))
	if err != nil || !bytes.Equal(got, []byte("n1")) {
		t.Fatalf("new1: %v %q", err, got)
	}
	got, err = os.ReadFile(filepath.Join(storeB, "projects", "renamed.txt"))
	if err != nil || !bytes.Equal(got, []byte("same")) {
		t.Fatalf("renamed: %v %q", err, got)
	}
	got, err = os.ReadFile(filepath.Join(storeB, "projects", "new3.txt"))
	if err != nil || !bytes.Equal(got, []byte("n3")) {
		t.Fatalf("new3: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(storeB, "projects", "deleteme.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleteme should be gone on B, err=%v", err)
	}
	still, _ := q2.CountPending(ctx)
	if still != 0 {
		t.Fatalf("pending after flush: %d", still)
	}
}

func TestOfflineChangePlusRemoteUsesTwoWayConflict(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	shareA := t.TempDir()
	shareB := t.TempDir()
	storeA := filepath.Join(shareA, "s")
	storeB := filepath.Join(shareB, "s")
	agentA := registerLiveAgent(t, ts.URL, cl, "home", shareA, storeA)
	agentB := registerLiveAgent(t, ts.URL, cl, "mac", shareB, storeB)
	defer agentB.stop()
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
		Name: "offline conflict", Mode: "two_way",
		SourceDeviceID: agentA.DeviceID, SourcePath: "projects",
		DestDeviceID: agentB.DeviceID, DestPath: "projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	put(agentA.DeviceID, "projects/shared.txt", []byte("v0"))
	runWait(t, cl, job.ID)

	agentData := t.TempDir()
	q, err := offline.Open(offline.Config{DBPath: offline.DefaultDBPath(agentData)})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	sc := offline.NewScanner(storeA, q)
	_ = sc.SeedBaseline(ctx)

	agentA.stop()
	time.Sleep(80 * time.Millisecond)

	// Offline local change on A
	if err := os.WriteFile(filepath.Join(storeA, "projects", "shared.txt"), []byte("from-a-offline"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// Remote change on B while A offline
	put(agentB.DeviceID, "projects/shared.txt", []byte("from-b-online"))

	agentA.stop = connectAgentWS(t, ts.URL, agentA, shareA, storeA)
	defer agentA.stop()
	time.Sleep(100 * time.Millisecond)

	flush, err := cl.FlushSync(ctx, agentA.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := cl.ListSyncConflicts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(flush.ConflictPaths) == 0 && len(list) == 0 {
		t.Fatalf("expected two-way conflict, flush=%+v", flush)
	}
	pending, _ := q.ListPending(ctx)
	ids := make([]string, len(pending))
	for i, e := range pending {
		ids[i] = e.ID
	}
	_ = q.MarkSyncing(ctx, ids)
	cmap := map[string]struct{}{}
	for _, p := range flush.ConflictPaths {
		cmap[p] = struct{}{}
	}
	for _, c := range list {
		cmap[c.RelPath] = struct{}{}
	}
	if len(cmap) == 0 {
		cmap["shared.txt"] = struct{}{}
		cmap["projects/shared.txt"] = struct{}{}
	}
	_ = q.FinishFlush(ctx, cmap)
	conf, _ := q.ListByStatus(ctx, offline.StatusConflict)
	if len(conf) == 0 {
		t.Fatal("expected journal CONFLICT status")
	}

	// existing resolution still works
	if len(list) == 0 {
		list, err = cl.ListSyncConflicts(ctx, job.ID)
		if err != nil || len(list) == 0 {
			t.Fatalf("conflicts: %v %d", err, len(list))
		}
	}
	if _, err := cl.ResolveSyncConflict(ctx, list[0].ID, "keep_both"); err != nil {
		t.Fatal(err)
	}
	// ResolveConflict schedules a re-run; wait for it to settle.
	wctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := cl.WaitSyncJob(wctx, job.ID, 80*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
