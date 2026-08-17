package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

func TestEdgeStreaming(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)

	const chunk = 64 << 10
	const chunks = 8 // 512 KiB streamed response
	var uploaded int64

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			fmt.Fprint(w, "ok-small")
		case "/stream":
			total := chunk * chunks
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(total))
			buf := make([]byte, total)
			for i := 0; i < chunks; i++ {
				for j := 0; j < chunk; j++ {
					buf[i*chunk+j] = byte(i)
				}
			}
			_, _ = w.Write(buf)
		case "/upload":
			n, _ := io.Copy(io.Discard, r.Body)
			atomic.StoreInt64(&uploaded, n)
			fmt.Fprintf(w, "got:%d", n)
		case "/slow":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "28")
			fl := w.(http.Flusher)
			for i := 0; i < 4; i++ {
				fmt.Fprintf(w, "part%d-", i)
				fl.Flush()
				time.Sleep(5 * time.Millisecond)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(origin.Close)
	port := originPort(t, origin)

	ctx := context.Background()
	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{Hostname: "example.com", ServiceID: web.ID}); err != nil {
		t.Fatal(err)
	}

	small := edgeGET(t, ts.Client(), ts.URL, "example.com", "/small")
	if small.Status != http.StatusOK || small.Body != "ok-small" {
		t.Fatalf("small: %d %q", small.Status, small.Body)
	}

	stream := edgeGET(t, ts.Client(), ts.URL, "example.com", "/stream")
	if stream.Status != http.StatusOK || len(stream.Body) != chunk*chunks {
		t.Fatalf("stream len=%d status=%d", len(stream.Body), stream.Status)
	}

	uploadSize := chunk * 3
	up := edgePOST(t, ts.Client(), ts.URL, "example.com", "/upload", makePayload(uploadSize))
	if up.Status != http.StatusOK || up.Body != fmt.Sprintf("got:%d", uploadSize) {
		t.Fatalf("upload: %d %q", up.Status, up.Body)
	}
	if atomic.LoadInt64(&uploaded) != int64(uploadSize) {
		t.Fatalf("origin saw %d bytes", uploaded)
	}

	const parallel = 6
	var wg sync.WaitGroup
	errs := make(chan error, parallel)
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := edgeGET(t, ts.Client(), ts.URL, "example.com", "/small")
			if r.Status != http.StatusOK || r.Body != "ok-small" {
				errs <- fmt.Errorf("parallel status=%d", r.Status)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// Origin generates response incrementally; Edge still completes the stream.
	slow := edgeGET(t, ts.Client(), ts.URL, "example.com", "/slow")
	if slow.Status != http.StatusOK || slow.Body != "part0-part1-part2-part3-" {
		t.Fatalf("slow stream: %d %q", slow.Status, slow.Body)
	}
}

func TestEdgeTunnelReconnect(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, token, pub, priv, stopHome := registerAndConnectFull(t, ts, cl, "Home PC", share)
	time.Sleep(80 * time.Millisecond)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "alive")
	}))
	t.Cleanup(origin.Close)
	port := originPort(t, origin)

	ctx := context.Background()
	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{Hostname: "example.com", ServiceID: web.ID}); err != nil {
		t.Fatal(err)
	}

	ok := edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	if ok.Status != http.StatusOK || ok.Body != "alive" {
		t.Fatalf("before disconnect: %d %q", ok.Status, ok.Body)
	}

	stopHome()
	time.Sleep(100 * time.Millisecond)

	down := edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	if down.Status != http.StatusServiceUnavailable && down.Status != http.StatusBadGateway {
		t.Fatalf("expected unavailable during outage, got %d", down.Status)
	}

	stopHome = reconnectAgent(t, ts, token, idHome, pub, priv, share, filepath.Join(share, "storage"), "Home PC")
	defer stopHome()
	time.Sleep(100 * time.Millisecond)

	again := edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	if again.Status != http.StatusOK || again.Body != "alive" {
		t.Fatalf("after reconnect: %d %q", again.Status, again.Body)
	}

	h, err := cl.ServiceHealth(ctx, web.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.TunnelConnected || !h.BackendReachable {
		t.Fatalf("health after reconnect: %+v", h)
	}
}

func TestEdgeInFlightAbort(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)

	block := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		fmt.Fprint(w, "late")
	}))
	t.Cleanup(origin.Close)
	port := originPort(t, origin)

	ctx := context.Background()
	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{Hostname: "example.com", ServiceID: web.ID}); err != nil {
		t.Fatal(err)
	}

	done := make(chan edgeResp, 1)
	go func() {
		done <- edgeGET(t, ts.Client(), ts.URL, "example.com", "/")
	}()

	time.Sleep(150 * time.Millisecond)
	stopHome()
	close(block)

	select {
	case r := <-done:
		if r.Status == http.StatusOK {
			t.Fatal("expected failure for in-flight after tunnel drop")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request hung")
	}
}

func originPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	ou, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(ou.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func edgePOST(t *testing.T, hc *http.Client, base, host, path string, body []byte) edgeResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.ContentLength = int64(len(body))
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return edgeResp{Status: resp.StatusCode, Body: string(raw), Header: resp.Header.Clone()}
}

func makePayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}
