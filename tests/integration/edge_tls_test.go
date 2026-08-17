package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/protocol"
)

func TestOriginTLSPassthrough(t *testing.T) {
	ts, cl, _, _, passthrough := startCPFull(t, true, t.TempDir()+"/knot.db")
	if passthrough == "" {
		t.Fatal("passthrough listener required")
	}
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS #3", t.TempDir())
	defer stopVPS()
	time.Sleep(100 * time.Millisecond)

	host := "origin-tls.example.com"
	port := freePort(t)
	bodyMarker := "origin-tls-secret-body"
	cert := mustCert(t, host)

	originLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = originLn.Close() })
	originSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, bodyMarker)
		}),
	}
	go func() {
		_ = originSrv.Serve(tls.NewListener(originLn, &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"http/1.1", "h2"},
		}))
	}()
	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	svc, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "tls-app", Kind: "web", Protocol: "https", Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: host, ServiceID: svc.ID, EdgeDeviceID: idVPS, TLSMode: protocol.TLSModeOriginTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.TLSMode != protocol.TLSModeOriginTLS {
		t.Fatalf("route tls_mode=%q", rt.TLSMode)
	}

	tlsConn, err := tls.Dial("tcp", passthrough, &tls.Config{
		ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tlsConn.Close()
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 || state.PeerCertificates[0].Subject.CommonName != host {
		t.Fatalf("expected origin cert for %s, got %+v", host, state.PeerCertificates[0].Subject)
	}

	if _, err := fmt.Fprintf(tlsConn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		t.Fatal(err)
	}
	_ = tlsConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	raw, err := io.ReadAll(tlsConn)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), bodyMarker) {
		t.Fatalf("body missing marker: %q", string(raw))
	}

	// Large streaming response over end-to-end TLS.
	largeHost := "large-tls.example.com"
	largePort := freePort(t)
	largeCert := mustCert(t, largeHost)
	largeLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", largePort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = largeLn.Close() })
	payloadSize := 256 << 10
	go func() {
		_ = (&http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			buf := make([]byte, 16<<10)
			for sent := 0; sent < payloadSize; {
				n := len(buf)
				if sent+n > payloadSize {
					n = payloadSize - sent
				}
				_, _ = w.Write(buf[:n])
				if fl != nil {
					fl.Flush()
				}
				sent += n
			}
		})}).Serve(tls.NewListener(largeLn, &tls.Config{Certificates: []tls.Certificate{largeCert}}))
	}()
	time.Sleep(50 * time.Millisecond)

	svc2, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "large-tls", Kind: "web", Protocol: "https", Port: largePort,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: largeHost, ServiceID: svc2.ID, EdgeDeviceID: idVPS, TLSMode: protocol.TLSModeOriginTLS,
	}); err != nil {
		t.Fatal(err)
	}
	lc, err := tls.Dial("tcp", passthrough, &tls.Config{ServerName: largeHost, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lc.Close()
	fmt.Fprintf(lc, "GET /stream HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", largeHost)
	streamBody, err := io.ReadAll(lc)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamBody) < payloadSize/2 {
		t.Fatalf("expected large TLS payload, got %d bytes", len(streamBody))
	}

	// Origin down → client sees handshake/connection failure.
	_ = originLn.Close()
	time.Sleep(50 * time.Millisecond)
	failConn, err := tls.Dial("tcp", passthrough, &tls.Config{ServerName: host, InsecureSkipVerify: true})
	if err == nil {
		failConn.Close()
		t.Fatal("expected failure with origin down")
	}

	// Reconnect tunnel.
	stopHome()
	time.Sleep(80 * time.Millisecond)
	idHome, stopHome = registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(100 * time.Millisecond)

	// edge_terminate regression on HTTP route still works.
	httpPort := freePort(t)
	origin := http.Server{
		Addr: fmt.Sprintf("127.0.0.1:%d", httpPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "edge-terminate-ok")
		}),
	}
	go origin.ListenAndServe()
	t.Cleanup(func() { _ = origin.Close() })
	time.Sleep(30 * time.Millisecond)

	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "plain-web", Kind: "web", Port: httpPort,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname: "terminate.example.com", ServiceID: web.ID, EdgeDeviceID: idVPS,
		TLSMode: protocol.TLSModeEdgeTerminate,
	}); err != nil {
		t.Fatal(err)
	}
	got := edgeGET(t, ts.Client(), ts.URL, "terminate.example.com", "/")
	if got.Status != http.StatusOK || got.Body != "edge-terminate-ok" {
		t.Fatalf("edge_terminate broken: %d %q", got.Status, got.Body)
	}
}

func mustCert(t *testing.T, host string) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
