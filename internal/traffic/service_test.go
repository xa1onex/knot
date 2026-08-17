package traffic

import (
	"testing"

	"github.com/knot-infra/knot/internal/store"
)

func TestPickTrafficReleaseID(t *testing.T) {
	targets := []store.RouteTrafficTarget{
		{ReleaseID: "a", Weight: 0},
		{ReleaseID: "b", Weight: 100},
	}
	for i := 0; i < 20; i++ {
		if got := store.PickTrafficReleaseID(targets, "a"); got != "b" {
			t.Fatalf("100/0 should always pick b, got %s", got)
		}
	}
	if got := store.PickTrafficReleaseID(nil, "fallback"); got != "fallback" {
		t.Fatalf("empty targets: %s", got)
	}
	failed := []store.RouteTrafficTarget{{ReleaseID: "bad", Weight: 0}}
	if got := store.PickTrafficReleaseID(failed, "live"); got != "live" {
		t.Fatalf("zero-weight must not win: %s", got)
	}
}
