package hardening_test

import (
	"testing"

	"github.com/knot-infra/knot/internal/hardening"
)

func TestCertReloaderMissingFiles(t *testing.T) {
	_, err := hardening.NewCertReloader("/no/such/cert.pem", "/no/such/key.pem")
	if err == nil {
		t.Fatal("expected error")
	}
}
