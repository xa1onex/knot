package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/agentws"
	"github.com/knot-infra/knot/internal/api"
	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/config"
	"github.com/knot-infra/knot/internal/devices"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/protocol"
)

func TestAgentControlPlaneFlow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knot.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Config{
		HTTPAddr:            "127.0.0.1:0",
		AccessTokenTTL:      time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
		DeviceSessionTTL:    time.Hour,
		HeartbeatTimeout:    45 * time.Second,
		BootstrapAdminEmail: "admin@node.local",
		BootstrapAdminPass:  "admin",
		CORSOrigin:          "*",
	}
	authSvc := &auth.Service{
		Store:            st,
		AccessTokenTTL:   cfg.AccessTokenTTL,
		RefreshTokenTTL:  cfg.RefreshTokenTTL,
		DeviceSessionTTL: cfg.DeviceSessionTTL,
	}
	if err := authSvc.EnsureBootstrapAdmin(context.Background(), cfg.BootstrapAdminEmail, cfg.BootstrapAdminPass); err != nil {
		t.Fatal(err)
	}
	devSvc := &devices.Service{Store: st, Auth: authSvc}
	auditLog := &audit.Logger{Store: st}
	hub := agentws.NewHub(authSvc, devSvc, st, auditLog)
	srv := &api.Server{Cfg: cfg, Store: st, Auth: authSvc, Devices: devSvc, Audit: auditLog, Hub: hub}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cl := client.New(ts.URL, "")
	login, err := cl.Login(context.Background(), "admin@node.local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if login.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}

	refreshed, err := cl.Refresh(context.Background(), login.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	cl.Token = refreshed.AccessToken

	reg, err := cl.CreateRegToken(context.Background(), "home-pc", 1)
	if err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	regBody, _ := json.Marshal(protocol.RegisterRequest{
		RegistrationToken: reg,
		PublicKey:         base64.RawURLEncoding.EncodeToString(pub),
		Name:              "home-pc",
		Hostname:          "test-host",
		OS:                "darwin",
		Arch:              "arm64",
	})
	resp, err := http.Post(ts.URL+"/v1/agent/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("register status %d", resp.StatusCode)
	}
	var regOut protocol.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regOut); err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/agent/connect?token=" + regOut.DeviceToken
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var challenge protocol.ChallengeMessage
	if err := json.Unmarshal(data, &challenge); err != nil || challenge.Type != protocol.TypeChallenge {
		t.Fatalf("challenge: %s", data)
	}
	sig := ed25519.Sign(priv, []byte(challenge.Nonce))
	if err := conn.WriteJSON(protocol.ChallengeResponse{
		Type: protocol.TypeChallengeR, Nonce: challenge.Nonce,
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	}); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var session protocol.SessionMessage
	if err := json.Unmarshal(data, &session); err != nil || session.Type != protocol.TypeSession {
		t.Fatalf("session: %s", data)
	}
	_ = conn.WriteJSON(protocol.Heartbeat{
		Type:      protocol.TypeHeartbeat,
		Telemetry: protocol.Telemetry{Hostname: "test-host", OS: "darwin", Arch: "arm64", CPUs: 2},
	})
	time.Sleep(100 * time.Millisecond)

	devices, err := cl.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || !devices[0].Online {
		t.Fatalf("expected online device, got %+v", devices)
	}

	_ = conn.Close()
	time.Sleep(150 * time.Millisecond)
	devices, err = cl.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Online {
		t.Fatal("expected offline after disconnect")
	}

	_, apiTok, err := cl.CreateCredential(context.Background(), "e2e", []string{"devices.read"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	apiCl := client.New(ts.URL, apiTok)
	if _, err := apiCl.ListDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, err = apiCl.CreateCredential(context.Background(), "x", []string{"devices.read"}, 1)
	if err == nil {
		t.Fatal("expected forbidden")
	}
	if ae, ok := err.(*client.APIError); !ok || ae.Code != apierrors.CodeForbidden {
		t.Fatalf("expected forbidden code, got %v", err)
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st2.Close()
	_ = os.Remove(path)
}

func TestErrorJSONShape(t *testing.T) {
	rec := httptest.NewRecorder()
	apierrors.WriteCode(rec, 401, apierrors.CodeTokenRevoked, "token revoked")
	code, msg := apierrors.ParseBody(rec.Body.Bytes())
	if code != apierrors.CodeTokenRevoked || msg != "token revoked" {
		t.Fatalf("%s %s", code, msg)
	}
}
