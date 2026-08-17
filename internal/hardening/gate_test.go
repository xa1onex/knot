package hardening_test

import (
	"net/http"
	"testing"

	"github.com/knot-infra/knot/internal/hardening"
)

func TestClientIPIgnoresForwardedUnlessTrusted(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.8:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := hardening.ClientIP(r, false); got != "10.0.0.8" {
		t.Fatalf("untrusted XFF: got %q", got)
	}
	if got := hardening.ClientIP(r, true); got != "1.2.3.4" {
		t.Fatalf("trusted XFF: got %q", got)
	}
}
