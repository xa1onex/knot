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
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/agent/buildrunner"
	"github.com/knot-infra/knot/internal/agent/deployrunner"
	"github.com/knot-infra/knot/internal/agent/edgeproxy"
	"github.com/knot-infra/knot/internal/agent/jobrunner"
	"github.com/knot-infra/knot/internal/agent/storfs"
	"github.com/knot-infra/knot/internal/agent/xfer"
	"github.com/knot-infra/knot/internal/agentws"
	"github.com/knot-infra/knot/internal/aisessions"
	"github.com/knot-infra/knot/internal/api"
	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/builds"
	"github.com/knot-infra/knot/internal/compute"
	"github.com/knot-infra/knot/internal/config"
	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/devices"
	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/files"
	"github.com/knot-infra/knot/internal/hardening"
	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/plans"
	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	syncjob "github.com/knot-infra/knot/internal/sync"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/internal/transfers"
	"github.com/knot-infra/knot/internal/workflows"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/protocol"
)

func startCP(t *testing.T, forceRelay bool) (*httptest.Server, *client.Client, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	ts, cl, st, _, _ := startCPFull(t, forceRelay, filepath.Join(dir, "knot.db"))
	return ts, cl, st
}

func startCPWithDB(t *testing.T, forceRelay bool, dbPath string) (*httptest.Server, *client.Client, *store.Store) {
	t.Helper()
	ts, cl, st, _, _ := startCPFull(t, forceRelay, dbPath)
	return ts, cl, st
}

func startCPFull(t *testing.T, forceRelay bool, dbPath string) (*httptest.Server, *client.Client, *store.Store, http.Handler, string) {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Config{
		HTTPAddr: "127.0.0.1:0", AccessTokenTTL: time.Hour, RefreshTokenTTL: 24 * time.Hour,
		DeviceSessionTTL: time.Hour, HeartbeatTimeout: 45 * time.Second,
		BootstrapAdminEmail: "admin@node.local", BootstrapAdminPass: "admin", CORSOrigin: "*",
		ForceRelay: forceRelay, DirectTimeout: 2 * time.Second,
		STUNURLs: []string{}, DatabasePath: dbPath,
	}
	authSvc := &auth.Service{Store: st, AccessTokenTTL: cfg.AccessTokenTTL, RefreshTokenTTL: cfg.RefreshTokenTTL, DeviceSessionTTL: cfg.DeviceSessionTTL}
	if err := authSvc.EnsureBootstrapAdmin(context.Background(), cfg.BootstrapAdminEmail, cfg.BootstrapAdminPass); err != nil {
		t.Fatal(err)
	}
	devSvc := &devices.Service{Store: st, Auth: authSvc}
	auditLog := &audit.Logger{Store: st}
	hub := agentws.NewHub(authSvc, devSvc, st, auditLog)
	xferSvc := transfers.New(st, hub, transfers.Options{
		ForceRelay: cfg.ForceRelay, STUNURLs: cfg.STUNURLs, DirectTimeout: cfg.DirectTimeout,
	})
	hub.SetTransfers(xferSvc)
	storageSvc := storage.New(st, hub, xferSvc)
	hub.SetStorage(storageSvc)
	syncSvc := syncjob.New(st, storageSvc)
	hub.SetOffline(syncjob.HubFlush{S: syncSvc})
	filesSvc := files.New(st, storageSvc, hub)
	hub.SetIndexer(filesSvc)
	storageSvc.OnMutate = filesSvc.OnMutate
	svcReg := services.New(st)
	edgeProxy := edge.New(st, hub)
	hub.SetEdge(edgeProxy)
	secKey, err := secrets.LoadOrCreateKey(dbPath, "", "")
	if err != nil {
		t.Fatal(err)
	}
	secretsSvc := secrets.New(st, secKey)
	envSvc := environments.New(st, secretsSvc)
	deploySvc := deploy.New(st, hub, svcReg, edgeProxy)
	deploySvc.Secrets = secretsSvc
	deploySvc.Envs = envSvc
	hub.SetDeploy(deploySvc)
	computeSvc := compute.New(st, cfg.HeartbeatTimeout)
	jobsSvc := jobs.New(st, hub, computeSvc)
	hub.SetJobs(jobsSvc)
	buildsSvc := builds.New(st, hub, secretsSvc)
	hub.SetBuilds(buildsSvc)
	relSvc := releases.New(st, deploySvc, envSvc, secretsSvc)
	relSvc.Builds = buildsSvc
	relSvc.Jobs = jobsSvc
	trafSvc := traffic.New(st)
	relSvc.Traffic = trafSvc
	logsSvc := oplogs.New(st, 30)
	auditLog.Logs = logsSvc
	hub.Logs = logsSvc
	edgeProxy.Logs = logsSvc
	deploySvc.Ops = logsSvc
	jobsSvc.Ops = logsSvc
	buildsSvc.Ops = logsSvc
	relSvc.Ops = logsSvc
	trafSvc.Logs = logsSvc
	opsSvc := ops.New(st, edgeProxy, trafSvc, logsSvc)
	wfSvc := workflows.New(st, opsSvc, trafSvc, relSvc, buildsSvc, jobsSvc, filesSvc, storageSvc, edgeProxy, logsSvc, auditLog)
	planSvc := plans.New(st, opsSvc, trafSvc, relSvc, buildsSvc, jobsSvc, filesSvc, storageSvc, edgeProxy, logsSvc, auditLog)
	aiSvc := aisessions.New(st, authSvc)
	srvAPI := &api.Server{Cfg: cfg, Store: st, Auth: authSvc, Devices: devSvc, Audit: auditLog, Hub: hub, Transfers: xferSvc, Storage: storageSvc, Sync: syncSvc, Files: filesSvc, Services: svcReg, Edge: edgeProxy, Environments: envSvc, Secrets: secretsSvc, Deploy: deploySvc, Releases: relSvc, Traffic: trafSvc, Builds: buildsSvc, Compute: computeSvc, Jobs: jobsSvc, Logs: logsSvc, Ops: opsSvc, Workflows: wfSvc, Plans: planSvc, AI: aiSvc, Gate: hardening.NewGate(200, 2000), Metrics: hardening.NewMetrics(), StartedAt: time.Now().UTC()}
	h := srvAPI.Handler()
	ptCtx, ptCancel := context.WithCancel(context.Background())
	t.Cleanup(ptCancel)
	passthroughAddr, _ := edgeProxy.StartPassthrough(ptCtx, "127.0.0.1:0")
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	cl := client.New(ts.URL, "")
	if _, err := cl.Login(context.Background(), "admin@node.local", "admin"); err != nil {
		t.Fatal(err)
	}
	return ts, cl, st, h, passthroughAddr
}

func registerAndConnect(t *testing.T, ts *httptest.Server, cl *client.Client, name, shareDir string) (deviceID string, cancel func()) {
	deviceID, _, _, _, cancel = registerAndConnectFull(t, ts, cl, name, shareDir)
	return deviceID, cancel
}

func registerAndConnectFull(t *testing.T, ts *httptest.Server, cl *client.Client, name, shareDir string) (deviceID, token string, pub ed25519.PublicKey, priv ed25519.PrivateKey, cancel func()) {
	t.Helper()
	storageDir := filepath.Join(shareDir, "storage")
	reg, err := cl.CreateRegToken(context.Background(), name, 1)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(protocol.RegisterRequest{
		RegistrationToken: reg, PublicKey: base64.RawURLEncoding.EncodeToString(pub),
		Name: name, Hostname: name, OS: "test", Arch: "amd64",
	})
	resp, err := http.Post(ts.URL+"/v1/agent/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var regOut protocol.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&regOut)
	deviceID, cancel = connectIntegrationAgentPolicy(t, ts, regOut.DeviceToken, regOut.DeviceID, pub, priv, shareDir, storageDir, name, jobrunner.TestPolicy())
	return deviceID, regOut.DeviceToken, pub, priv, cancel
}

func registerAndConnectWithJobPolicy(t *testing.T, ts *httptest.Server, cl *client.Client, name, shareDir string, pol jobrunner.Policy) (deviceID string, cancel func()) {
	t.Helper()
	storageDir := filepath.Join(shareDir, "storage")
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
	resp, err := http.Post(ts.URL+"/v1/agent/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var regOut protocol.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&regOut)
	deviceID, cancel = connectIntegrationAgentPolicy(t, ts, regOut.DeviceToken, regOut.DeviceID, pub, priv, shareDir, storageDir, name, pol)
	return deviceID, cancel
}

func reconnectAgent(t *testing.T, ts *httptest.Server, token, deviceID string, pub ed25519.PublicKey, priv ed25519.PrivateKey, shareDir, storageDir, name string) func() {
	t.Helper()
	_, cancel := connectIntegrationAgentPolicy(t, ts, token, deviceID, pub, priv, shareDir, storageDir, name, jobrunner.TestPolicy())
	return cancel
}

func connectIntegrationAgent(t *testing.T, ts *httptest.Server, deviceToken, deviceID string, pub ed25519.PublicKey, priv ed25519.PrivateKey, shareDir, storageDir, name string) (string, func()) {
	return connectIntegrationAgentPolicy(t, ts, deviceToken, deviceID, pub, priv, shareDir, storageDir, name, jobrunner.TestPolicy())
}

func connectIntegrationAgentPolicy(t *testing.T, ts *httptest.Server, deviceToken, deviceID string, pub ed25519.PublicKey, priv ed25519.PrivateKey, shareDir, storageDir, name string, pol jobrunner.Policy) (string, func()) {
	t.Helper()
	wsURL := "ws" + ts.URL[len("http"):] + "/v1/agent/connect?token=" + deviceToken
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
	sig := ed25519.Sign(priv, []byte(challenge.Nonce))
	_ = conn.WriteJSON(protocol.ChallengeResponse{
		Type: protocol.TypeChallengeR, Nonce: challenge.Nonce,
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	})
	_, _, _ = conn.ReadMessage() // session
	_, _, _ = conn.ReadMessage() // welcome

	writeMu := &sync.Mutex{}
	xf, err := xfer.NewManager(deviceID, shareDir, storageDir, pub, priv, conn, writeMu)
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
	ep := &edgeproxy.Manager{Send: func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}}
	dr := deployrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, deployrunner.NewComposite())
	jr := jobrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, storageDir)
	jr.Policy = pol
	fake := jobrunner.NewFakeRunner(storageDir)
	fake.Policy = pol
	jr.Runner = fake
	br := buildrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, filepath.Join(storageDir, "builds"))

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
			case protocol.TypeEdgeHTTPBegin, protocol.TypeEdgeHTTPBody, protocol.TypeEdgeHTTPAck, protocol.TypeEdgeHTTPFail,
				protocol.TypeEdgeProbe:
				ep.Handle(msg)
			case protocol.TypeEdgeStreamOpen, protocol.TypeEdgeStreamData, protocol.TypeEdgeStreamAck,
				protocol.TypeEdgeStreamClose, protocol.TypeEdgeStreamFail:
				ep.Handle(msg)
			case protocol.TypeDeployApply:
				dr.Handle(msg)
			case protocol.TypeJobRun, protocol.TypeJobCancel:
				jr.Handle(msg)
			case protocol.TypeBuildRun, protocol.TypeBuildCancel:
				br.Handle(msg)
			}
		}
	}()

	_ = conn.WriteJSON(protocol.Heartbeat{Type: protocol.TypeHeartbeat, Telemetry: protocol.Telemetry{Hostname: name, OS: "test", Arch: "amd64"}})

	return deviceID, func() {
		_ = conn.Close()
		<-done
		time.Sleep(50 * time.Millisecond)
	}
}

func registerAndConnectStorage(t *testing.T, ts *httptest.Server, cl *client.Client, name, shareDir, storageDir string) (deviceID string, cancel func()) {
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
	resp, err := http.Post(ts.URL+"/v1/agent/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var regOut protocol.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&regOut)

	return connectIntegrationAgent(t, ts, regOut.DeviceToken, regOut.DeviceID, pub, priv, shareDir, storageDir, name)
}

func runTransfer(t *testing.T, forceRelay bool) string {
	t.Helper()
	ts, cl, _ := startCP(t, forceRelay)
	shareA := t.TempDir()
	shareB := t.TempDir()
	idA, stopA := registerAndConnect(t, ts, cl, "vps", shareA)
	idB, stopB := registerAndConnect(t, ts, cl, "home", shareB)
	defer stopA()
	defer stopB()
	time.Sleep(150 * time.Millisecond)

	payload := []byte("hello from vps to home")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	_ = os.WriteFile(filepath.Join(shareA, "outbox", "note.txt"), payload, 0o600)

	tr, err := cl.CreateTransfer(context.Background(), idA, idB, "note.txt", "note.txt", int64(len(payload)), hexSum)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done, err := cl.WaitTransfer(ctx, tr.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" {
		t.Fatalf("status=%s err=%s path=%s", done.Status, done.Error, done.Path)
	}
	got, err := os.ReadFile(filepath.Join(shareB, "inbox", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch")
	}
	return done.Path
}

func TestRoundTripTransferRelayForced(t *testing.T) {
	path := runTransfer(t, true)
	if path != "relay" {
		t.Fatalf("expected relay path, got %q", path)
	}
}

func TestTransferDirectPreferred(t *testing.T) {
	path := runTransfer(t, false)
	// On same host, direct should usually win; if flaky env falls back to relay, still OK for data.
	if path != "direct" && path != "relay" {
		t.Fatalf("unexpected path %q", path)
	}
	t.Logf("transport path=%s", path)
}
