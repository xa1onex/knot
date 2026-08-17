package integration_test

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/pkg/client"
)

func TestHardeningReadyzAndBackup(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()
	if err := cl.Readyz(ctx); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics %d", resp.StatusCode)
	}
	path, err := cl.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestHardeningLoginLockout(t *testing.T) {
	ts, _, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()
	anon := client.New(ts.URL, "")
	for i := 0; i < auth.MaxLoginFails; i++ {
		_, err := anon.Login(ctx, "admin@node.local", "wrong-password")
		if err == nil {
			t.Fatal("expected failed login")
		}
	}
	_, err := anon.Login(ctx, "admin@node.local", "wrong-password")
	if err == nil {
		t.Fatal("expected lockout")
	}
	if !client.IsCode(err, "account_locked") {
		t.Fatalf("expected account_locked, got %v", err)
	}
	_, err = anon.Login(ctx, "admin@node.local", "admin")
	if err == nil {
		t.Fatal("locked account must not login even with correct password")
	}
	if !client.IsCode(err, "account_locked") {
		t.Fatalf("correct password still locked, got %v", err)
	}
}

func TestAuditRedactsSecrets(t *testing.T) {
	if got := audit.Sanitize("token=abc123 password=hunter2"); got == "token=abc123 password=hunter2" {
		t.Fatal("expected redaction")
	}
	if got := audit.Sanitize("token=abc123"); got != "token=[redacted]" {
		t.Fatalf("got %q", got)
	}
}
