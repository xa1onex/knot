package integration_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
	"github.com/knot-infra/knot/pkg/protocol"
)

func TestTrafficSwitchBlueGreen(t *testing.T) {
	ts, cl, _, handler, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	port1 := freePort(t)
	port2 := freePort(t)
	port3 := freePort(t)
	host := "traffic.example.com"

	r1, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: port1, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, err = cl.DeployRelease(ctx, r1.ID, "", 0)
	if err != nil || r1.Status != "active" {
		t.Fatalf("r1 deploy: %+v err=%v", r1, err)
	}

	sw, err := cl.SwitchRouteTraffic(ctx, host, r1.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if sw.ActiveReleaseID != r1.ID || sw.Hostname != host {
		t.Fatalf("switch r1: %+v", sw)
	}
	got := edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Status != http.StatusOK || got.Body != "v1" {
		t.Fatalf("edge after r1: status=%d body=%q", got.Status, got.Body)
	}

	r2, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v2", DeviceID: idHome, Port: port2,
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err = cl.DeployRelease(ctx, r2.ID, "", 0)
	if err != nil || r2.Status != "active" {
		t.Fatalf("r2 candidate: %+v err=%v", r2, err)
	}
	got = edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Status != http.StatusOK || got.Body != "v1" {
		t.Fatalf("candidate must not take traffic: status=%d body=%q", got.Status, got.Body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port2)); body != "v2" {
		t.Fatalf("green must be running: %q", body)
	}

	sw, err = cl.SwitchRouteTraffic(ctx, host, r2.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if sw.ActiveReleaseID != r2.ID || sw.PrevReleaseID != r1.ID {
		t.Fatalf("switch r2: %+v", sw)
	}
	got = edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Status != http.StatusOK || got.Body != "v2" {
		t.Fatalf("edge after switch: status=%d body=%q", got.Status, got.Body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port1)); body != "v1" {
		t.Fatalf("blue must stay up (0 downtime): %q", body)
	}

	rb, err := cl.RollbackRouteTraffic(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if rb.ActiveReleaseID != r1.ID {
		t.Fatalf("traffic rollback: %+v", rb)
	}
	got = edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Status != http.StatusOK || got.Body != "v1" {
		t.Fatalf("edge after traffic rollback: status=%d body=%q", got.Status, got.Body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port2)); body != "v2" {
		t.Fatalf("rollback must not redeploy/stop green: %q", body)
	}
	st, err := cl.GetRouteTraffic(ctx, host)
	if err != nil || len(st.History) < 2 {
		t.Fatalf("history: %v %+v", err, st)
	}

	bad, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v2-unhealthy", DeviceID: idHome, Port: port3,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err = cl.DeployRelease(ctx, bad.ID, "", 0)
	if err != nil || bad.Status != "failed" {
		t.Fatalf("failed candidate: %+v err=%v", bad, err)
	}
	if _, err := cl.SwitchRouteTraffic(ctx, host, bad.ID, 100); err == nil {
		t.Fatal("failed candidate must not receive traffic")
	}
	got = edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Body != "v1" {
		t.Fatalf("failed candidate leaked traffic: %q", got.Body)
	}

	_, tokRel, err := cl.CreateCredential(ctx, "rel-only", []string{permissions.ReleaseRead, permissions.ReleaseWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	relCl := client.New(ts.URL, tokRel)
	if _, err := relCl.SwitchRouteTraffic(ctx, host, r2.ID, 100); err == nil || !client.IsForbidden(err) {
		t.Fatalf("release.write must not switch traffic: %v", err)
	}
	_, tokTraf, err := cl.CreateCredential(ctx, "traf-only", []string{permissions.TrafficRead, permissions.TrafficWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	trafCl := client.New(ts.URL, tokTraf)
	if _, err := trafCl.CreateRelease(ctx, client.CreateReleaseRequest{Service: "web-app", Image: "knot-fake:v1"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("traffic.write must not create release: %v", err)
	}
	if _, err := trafCl.SwitchRouteTraffic(ctx, host, r2.ID, 100); err != nil {
		t.Fatalf("traffic.write should switch: %v", err)
	}
	_ = handler
}

func TestTrafficSwitchOriginTLS(t *testing.T) {
	ts, cl, _, _, passthrough := startCPFull(t, true, t.TempDir()+"/knot.db")
	if passthrough == "" {
		t.Fatal("passthrough required")
	}
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)

	host := "cutover-tls.example.com"
	tlsPort := freePort(t)
	marker := "origin-tls-alive"
	cert := mustCert(t, host)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", tlsPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		_ = (&http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, marker)
		})}).Serve(tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}))
	}()
	time.Sleep(40 * time.Millisecond)

	ctx := context.Background()
	svc, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "tls-cutover", Kind: "web", Protocol: "https", Port: tlsPort,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: host, ServiceID: svc.ID, TLSMode: protocol.TLSModeOriginTLS,
	}); err != nil {
		t.Fatal(err)
	}
	if body := originTLSGET(t, passthrough, host); !contains(body, marker) {
		t.Fatalf("unbound origin_tls uses service origin: %q", body)
	}
	st, err := cl.GetRouteTraffic(ctx, host)
	if err != nil || st.TLSMode != protocol.TLSModeOriginTLS || st.Hostname != host {
		t.Fatalf("traffic status: %+v err=%v", st, err)
	}

	httpPort := freePort(t)
	rel, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "other-app", Image: "knot-fake:v1", DeviceID: idHome, Port: httpPort,
	})
	if err != nil {
		t.Fatal(err)
	}
	rel, err = cl.DeployRelease(ctx, rel.ID, "", 0)
	if err != nil || rel.Status != "active" {
		t.Fatalf("other release: %+v err=%v", rel, err)
	}
	if _, err := cl.SwitchRouteTraffic(ctx, host, rel.ID, 100); err == nil {
		t.Fatal("must not bind a release from a different service")
	}
	if body := originTLSGET(t, passthrough, host); !contains(body, marker) {
		t.Fatalf("origin_tls still on service origin: %q", body)
	}
}

func originTLSGET(t *testing.T, addr, host string) string {
	t.Helper()
	c, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
	raw, _ := io.ReadAll(c)
	return string(raw)
}
