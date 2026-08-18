package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/config"
)

func TestValidateBindLoopbackOK(t *testing.T) {
	cfg := config.Config{HTTPAddr: "127.0.0.1:8787"}
	if err := cfg.ValidateBind(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBindPublicRequiresTLS(t *testing.T) {
	cfg := config.Config{HTTPAddr: "0.0.0.0:8787"}
	err := cfg.ValidateBind()
	if err == nil || !strings.Contains(err.Error(), "insecure_config") {
		t.Fatalf("expected insecure_config, got %v", err)
	}
}

func TestValidateBindPublicWithTLS(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:    "0.0.0.0:8787",
		TLSCertFile: "/tmp/cert",
		TLSKeyFile:  "/tmp/key",
	}
	if err := cfg.ValidateBind(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBootstrapWeakPassword(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:            "0.0.0.0:8787",
		BootstrapAdminEmail: "a@b.c",
		BootstrapAdminPass:  "admin",
		AllowInsecureBind:   true,
	}
	err := cfg.ValidateBootstrap(0)
	if err == nil || !strings.Contains(err.Error(), "insecure_config") {
		t.Fatalf("expected reject weak password, got %v", err)
	}
}

func TestValidateBootstrapLoopbackAllowsAdmin(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:            "127.0.0.1:8787",
		BootstrapAdminEmail: "a@b.c",
		BootstrapAdminPass:  "admin",
	}
	if err := cfg.ValidateBootstrap(0); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCORSFollowsPublicBaseURL(t *testing.T) {
	t.Setenv("KNOT_CORS_ORIGIN", "")
	t.Setenv("KNOT_PUBLIC_BASE_URL", "https://node.example.com")
	cfg := config.Load()
	if cfg.CORSOrigin != "https://node.example.com" {
		t.Fatalf("CORSOrigin=%q", cfg.CORSOrigin)
	}
}

func TestLoadDefaultAccessTokenTTLIsPersistent(t *testing.T) {
	t.Setenv("KNOT_ACCESS_TOKEN_TTL", "")
	cfg := config.Load()
	if cfg.AccessTokenTTL < 365*24*time.Hour {
		t.Fatalf("AccessTokenTTL=%s", cfg.AccessTokenTTL)
	}
}
