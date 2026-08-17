package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutboundTLSDefaultNil(t *testing.T) {
	t.Setenv("KNOT_TLS_CA", "")
	t.Setenv("KNOT_TLS_INSECURE", "")
	cfg, err := outboundTLS()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected default system TLS")
	}
}

func TestOutboundTLSInsecure(t *testing.T) {
	t.Setenv("KNOT_TLS_INSECURE", "1")
	t.Setenv("KNOT_TLS_CA", "")
	cfg, err := outboundTLS()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify")
	}
}

func TestOutboundTLSCA(t *testing.T) {
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	// Minimal invalid file should error; valid PEM from empty should error too.
	if err := os.WriteFile(ca, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KNOT_TLS_CA", ca)
	t.Setenv("KNOT_TLS_INSECURE", "")
	if _, err := outboundTLS(); err == nil {
		t.Fatal("expected error for garbage CA")
	}
}
