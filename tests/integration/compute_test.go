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
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
	"github.com/knot-infra/knot/pkg/protocol"
)

func TestComputeRegistryFromHeartbeat(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()

	id, conn, token, pub, priv, cancel := registerConnectRaw(t, ts, cl, "home-pc")
	defer cancel()

	sendComputeHeartbeat(t, conn, protocol.ComputeInventory{
		CPU:    protocol.ComputeCPU{Cores: 16, Architecture: "amd64", UsagePercent: f64(8)},
		Memory: protocol.ComputeMemory{TotalBytes: 64 << 30, AvailableBytes: 20 << 30, UsedBytes: 44 << 30},
		GPU: &[]protocol.ComputeGPU{{
			Vendor: "NVIDIA", Model: "RTX 4090", VRAMBytes: u64(12 << 30), Available: bptr(true),
		}},
		Disks: []protocol.ComputeDisk{
			{Mount: "C:", TotalBytes: 4 << 40, FreeBytes: 2400 << 30, UsedBytes: (4 << 40) - (2400 << 30)},
			{Mount: "D:", TotalBytes: 2 << 40, FreeBytes: 1 << 40, UsedBytes: 1 << 40},
		},
	}, "windows", "amd64", 16, 65536, "0.8.1")
	time.Sleep(150 * time.Millisecond)

	got, err := cl.GetComputeDevice(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != protocol.ComputeStatusAvailable {
		t.Fatalf("status %s", got.Status)
	}
	if got.CPU == nil || got.CPU.Cores != 16 || got.CPU.Architecture != "amd64" {
		t.Fatalf("cpu %+v", got.CPU)
	}
	if got.Memory == nil || got.Memory.TotalBytes != 64<<30 {
		t.Fatalf("mem %+v", got.Memory)
	}
	if got.GPU == nil || len(*got.GPU) != 1 || (*got.GPU)[0].Model != "RTX 4090" {
		t.Fatalf("gpu %+v", got.GPU)
	}
	if len(got.Disks) != 2 || got.Disks[0].Mount != "C:" {
		t.Fatalf("disks %+v", got.Disks)
	}
	if got.OS != "windows" || got.AgentVersion != "0.8.1" {
		t.Fatalf("identity os=%s agent=%s", got.OS, got.AgentVersion)
	}
	if got.LastTelemetryAt == nil {
		t.Fatal("last_telemetry_at missing")
	}

	list, err := cl.ListComputeDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DeviceID != id {
		t.Fatalf("list %+v", list)
	}

	cancel()
	time.Sleep(150 * time.Millisecond)

	offline, err := cl.GetComputeDevice(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if offline.Status != protocol.ComputeStatusOffline {
		t.Fatalf("expected offline snapshot, got %s", offline.Status)
	}
	if offline.CPU == nil || offline.CPU.Cores != 16 {
		t.Fatal("snapshot must survive disconnect")
	}

	conn2, cancel2 := dialAgent(t, ts, token, pub, priv)
	defer cancel2()
	sendComputeHeartbeat(t, conn2, protocol.ComputeInventory{
		CPU:    protocol.ComputeCPU{Cores: 16, Architecture: "amd64"},
		Memory: protocol.ComputeMemory{TotalBytes: 64 << 30, AvailableBytes: 30 << 30, UsedBytes: 34 << 30},
		GPU: &[]protocol.ComputeGPU{{
			Vendor: "NVIDIA", Model: "RTX 4090", VRAMBytes: u64(12 << 30),
		}},
		Disks: []protocol.ComputeDisk{{Mount: "C:", TotalBytes: 4 << 40, FreeBytes: 2 << 40, UsedBytes: 2 << 40}},
	}, "windows", "amd64", 16, 65536, "0.8.1")
	time.Sleep(150 * time.Millisecond)

	restored, err := cl.GetComputeDevice(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != protocol.ComputeStatusAvailable {
		t.Fatalf("after reconnect want available, got %s", restored.Status)
	}
	if restored.Memory == nil || restored.Memory.AvailableBytes != 30<<30 {
		t.Fatalf("snapshot should refresh on reconnect: %+v", restored.Memory)
	}

	_, tok, err := cl.CreateCredential(ctx, "no-compute", []string{permissions.DevicesRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tok)
	if _, err := ro.ListComputeDevices(ctx); err == nil || !client.IsForbidden(err) {
		t.Fatalf("expected forbidden without compute.read, got %v", err)
	}
}

func TestComputeRegistryGPUUnknownIsNull(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()
	id, conn, _, _, _, cancel := registerConnectRaw(t, ts, cl, "mac-mini")
	defer cancel()

	sendComputeHeartbeat(t, conn, protocol.ComputeInventory{
		CPU:    protocol.ComputeCPU{Cores: 8, Architecture: "arm64"},
		Memory: protocol.ComputeMemory{TotalBytes: 16 << 30, AvailableBytes: 8 << 30, UsedBytes: 8 << 30},
		GPU:    nil,
		Disks:  []protocol.ComputeDisk{{Mount: "/", TotalBytes: 1 << 40, FreeBytes: 400 << 30, UsedBytes: 624 << 30}},
	}, "darwin", "arm64", 8, 16384, protocol.AgentVersion)
	time.Sleep(150 * time.Millisecond)

	got, err := cl.GetComputeDevice(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.GPU != nil {
		t.Fatalf("gpu must be null, got %+v", got.GPU)
	}
	if got.CPU == nil || got.CPU.Cores != 8 {
		t.Fatalf("cpu %+v", got.CPU)
	}
}

func sendComputeHeartbeat(t *testing.T, conn *websocket.Conn, inv protocol.ComputeInventory, osName, arch string, cpus int, ramMB uint64, version string) {
	t.Helper()
	if err := conn.WriteJSON(protocol.Heartbeat{
		Type: protocol.TypeHeartbeat,
		Telemetry: protocol.Telemetry{
			Hostname: "node", OS: osName, Arch: arch, CPUs: cpus, RAMMB: ramMB,
			Version: version, Compute: &inv,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func registerConnectRaw(t *testing.T, ts *httptest.Server, cl *client.Client, name string) (
	deviceID string, conn *websocket.Conn, token string, pub ed25519.PublicKey, priv ed25519.PrivateKey, cancel func(),
) {
	t.Helper()
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
	conn, cancel = dialAgent(t, ts, regOut.DeviceToken, pub, priv)
	return regOut.DeviceID, conn, regOut.DeviceToken, pub, priv, cancel
}

func dialAgent(t *testing.T, ts *httptest.Server, deviceToken string, pub ed25519.PublicKey, priv ed25519.PrivateKey) (*websocket.Conn, func()) {
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	return conn, func() {
		_ = conn.Close()
		<-done
		time.Sleep(50 * time.Millisecond)
	}
}

func f64(v float64) *float64 { return &v }
func u64(v uint64) *uint64   { return &v }
func bptr(v bool) *bool      { return &v }
