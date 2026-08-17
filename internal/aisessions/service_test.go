package aisessions

import (
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/permissions"
)

func TestFilterGrantableSubset(t *testing.T) {
	granted := []string{permissions.LogsRead, permissions.ReleaseRead, permissions.CredentialsRW}
	ok, msg := permissions.FilterGrantable(granted, []string{permissions.LogsRead, permissions.TrafficWrite})
	if msg == "" || len(ok) != 0 {
		t.Fatalf("must not expand to traffic.write: %v %q", ok, msg)
	}
	ok, msg = permissions.FilterGrantable(granted, []string{permissions.LogsRead})
	if msg != "" || len(ok) != 1 || ok[0] != permissions.LogsRead {
		t.Fatalf("subset logs.read: %v %q", ok, msg)
	}
	_, msg = permissions.FilterGrantable(permissions.SessionScopes(), []string{permissions.AccountAdmin})
	if msg == "" {
		t.Fatal("admin must not be grantable to AI")
	}
}

func TestParseTTL(t *testing.T) {
	d, err := ParseTTL(0, "")
	if err != nil || d != DefaultTTL {
		t.Fatalf("default: %v %v", d, err)
	}
	d, err = ParseTTL(0, "1s")
	if err != nil || d != time.Second {
		t.Fatalf("1s: %v %v", d, err)
	}
	d, err = ParseTTL(30, "")
	if err != nil || d != 30*time.Minute {
		t.Fatalf("30m: %v %v", d, err)
	}
	if _, err := ParseTTL(0, "nope"); err == nil {
		t.Fatal("invalid ttl must fail")
	}
}
