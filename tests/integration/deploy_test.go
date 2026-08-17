package integration_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestDeployLifecycleEdge(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS #3", t.TempDir())
	defer stopVPS()
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	port := freePort(t)

	v1, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: "knot-fake:v1", Port: port,
		HealthPath: "/health", Env: map[string]string{"API_SECRET": "s3cr3t"},
		Hostname: "deploy.example.com", EdgeDeviceID: idVPS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v1.Active || !v1.HealthOK || v1.Status != "running" {
		t.Fatalf("v1: %+v", v1)
	}
	if v1.Env["API_SECRET"] != "[redacted]" {
		t.Fatalf("env not redacted: %+v", v1.Env)
	}
	if v1.ServiceID == "" {
		t.Fatal("expected service registry entry")
	}

	h, err := cl.ServiceHealth(ctx, v1.ServiceID)
	if err != nil || !h.BackendReachable {
		t.Fatalf("service health: %+v err=%v", h, err)
	}

	got := edgeGET(t, ts.Client(), ts.URL, "deploy.example.com", "/health")
	if got.Status != http.StatusOK || got.Body != "v1" {
		t.Fatalf("edge v1: status=%d body=%q", got.Status, got.Body)
	}

	logs, err := cl.DeploymentLogs(ctx, v1.ID, 20)
	if err != nil || len(logs) == 0 {
		t.Fatalf("logs: %v len=%d", err, len(logs))
	}
	for _, l := range logs {
		if l.Message != "" && containsSecret(l.Message) {
			t.Fatalf("secret in logs: %q", l.Message)
		}
	}

	v2bad, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: "knot-fake:v2-unhealthy", Port: port,
		HealthPath: "/health",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v2bad.Active || v2bad.Image != "knot-fake:v1" {
		t.Fatalf("expected auto-rollback to v1, got %+v", v2bad)
	}

	got2 := edgeGET(t, ts.Client(), ts.URL, "deploy.example.com", "/")
	if got2.Status != http.StatusOK || got2.Body != "v1" {
		t.Fatalf("after rollback: status=%d body=%q", got2.Status, got2.Body)
	}

	restarted, err := cl.RestartDeployment(ctx, v2bad.ID)
	if err != nil || !restarted.HealthOK {
		t.Fatalf("restart: %+v err=%v", restarted, err)
	}

	stopped, err := cl.StopDeployment(ctx, v2bad.ID)
	if err != nil || stopped.Status != "stopped" {
		t.Fatalf("stop: %+v err=%v", stopped, err)
	}

	stopHome()
	time.Sleep(80 * time.Millisecond)

	active, err := cl.GetDeployment(ctx, v2bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != "stopped" || active.HealthOK {
		t.Fatalf("disconnect cleanup: %+v", active)
	}

	down := edgeGET(t, ts.Client(), ts.URL, "deploy.example.com", "/")
	if down.Status != http.StatusServiceUnavailable && down.Status != http.StatusBadGateway {
		t.Fatalf("edge offline expected 503/502, got %d", down.Status)
	}
}

func containsSecret(msg string) bool {
	return len(msg) > 0 && (contains(msg, "s3cr3t") || contains(msg, "API_SECRET=s3cr3t"))
}

func contains(s, sub string) bool {
	return len(sub) <= len(s) && searchSub(s, sub)
}

func searchSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
