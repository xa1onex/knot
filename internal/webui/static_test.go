package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWrapServesPanelOnPublicHost(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("PANEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Handler", "api")
		_, _ = w.Write([]byte("API:" + r.URL.Path))
	})
	h := Wrap(api, dir)

	get := func(path, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/", "node.example.com")
	if rec.Code != 200 {
		t.Fatalf("panel status %d", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "PANEL" {
		t.Fatalf("panel body %q", body)
	}

	rec = get("/files", "203.0.113.10:443")
	if rec.Code != 200 {
		t.Fatalf("spa status %d", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "PANEL" {
		t.Fatalf("spa body %q", body)
	}

	rec = get("/assets/app.js", "node.example.com")
	if rec.Code != 200 {
		t.Fatalf("asset status %d", rec.Code)
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "JS" {
		t.Fatalf("asset body %q", body)
	}

	rec = get("/v1/devices", "node.example.com")
	if rec.Header().Get("X-Handler") != "api" {
		t.Fatal("expected API for /v1/devices")
	}

	rec = get("/healthz", "node.example.com")
	if rec.Header().Get("X-Handler") != "api" {
		t.Fatal("expected API for /healthz")
	}
}

func TestIsAPIPath(t *testing.T) {
	if !isAPIPath("/v1/auth/login") || !isAPIPath("/healthz") || isAPIPath("/") || isAPIPath("/settings") {
		t.Fatal("api path classification")
	}
}
