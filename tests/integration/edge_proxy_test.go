package integration_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

func TestEdgeReverseProxy(t *testing.T) {
	ts, cl, _, handler, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS #3", t.TempDir())
	defer stopVPS()
	time.Sleep(80 * time.Millisecond)

	originBody := "hello-from-home"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Origin", "home-pc")
		if r.URL.Path == "/v1/secret" {
			fmt.Fprint(w, "origin-api")
			return
		}
		fmt.Fprint(w, originBody)
	}))
	t.Cleanup(origin.Close)
	ou, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(ou.Port())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	pg, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "postgres", Kind: "database", Port: 5432,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: "example.com", ServiceID: pg.ID, EdgeDeviceID: idVPS,
	}); err == nil || !client.IsCode(err, "validation_error") {
		t.Fatalf("expected tcp service rejected, got %v", err)
	}

	rt, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: "example.com", ServiceID: web.ID, EdgeDeviceID: idVPS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Hostname != "example.com" || rt.ServiceID != web.ID {
		t.Fatalf("%+v", rt)
	}

	listed, err := cl.ListRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Hostname != "example.com" {
		t.Fatalf("%+v", listed)
	}

	h, err := cl.ServiceHealth(ctx, web.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Registered || !h.AgentOnline || !h.TunnelConnected || !h.BackendReachable {
		t.Fatalf("health %+v", h)
	}
	if h.EdgeDeviceName != "VPS #3" || len(h.Hostnames) != 1 || h.Hostnames[0] != "example.com" {
		t.Fatalf("edge %+v", h)
	}

	got := edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	if got.Status != http.StatusOK || got.Body != originBody {
		t.Fatalf("http host route: status=%d body=%q", got.Status, got.Body)
	}
	if got.Header.Get("X-Origin") != "home-pc" {
		t.Fatalf("missing origin header: %v", got.Header)
	}

	// Public hostname must not leak the Node API — /v1 goes to the origin.
	apiLeak := edgeGET(t, ts.Client(), ts.URL, "example.com", "/v1/secret")
	if apiLeak.Body != "origin-api" {
		t.Fatalf("leaked Node API on public host: %q", apiLeak.Body)
	}

	tlsTS := httptest.NewTLSServer(handler)
	t.Cleanup(tlsTS.Close)
	httpsGot := edgeGET(t, tlsTS.Client(), tlsTS.URL, "example.com", "/")
	if httpsGot.Status != http.StatusOK || httpsGot.Body != originBody {
		t.Fatalf("https edge: status=%d body=%q", httpsGot.Status, httpsGot.Body)
	}

	// Home agent gone (NAT / Wi-Fi drop). Origin still listens on loopback.
	// If Control Plane dialed 127.0.0.1 itself this would still be 200.
	stopHome()
	time.Sleep(80 * time.Millisecond)

	down := edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	if down.Status != http.StatusServiceUnavailable && down.Status != http.StatusBadGateway {
		t.Fatalf("expected 503/502 with agent offline, got %d body=%q", down.Status, down.Body)
	}

	h2, err := cl.ServiceHealth(ctx, web.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h2.AgentOnline || h2.TunnelConnected || h2.BackendReachable {
		t.Fatalf("expected tunnel down, got %+v", h2)
	}
	if !h2.Registered {
		t.Fatal("service should remain registered")
	}

	// Unmatched Host still serves Node API.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz=%d", resp.StatusCode)
	}
}

type edgeResp struct {
	Status int
	Body   string
	Header http.Header
}

func edgeGET(t *testing.T, hc *http.Client, base, host, path string) edgeResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return edgeResp{Status: resp.StatusCode, Body: string(raw), Header: resp.Header.Clone()}
}
