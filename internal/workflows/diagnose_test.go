package workflows

import (
	"testing"

	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/store"
)

func TestDiagnoseRecommendationFailedCandidate(t *testing.T) {
	st := &runState{
		view:    &ops.Context{Status: "degraded", Summary: "Service: web-app"},
		current: &store.Release{ID: "r1", Number: 43, Status: store.ReleaseStatusActive},
		latest:  &store.Release{ID: "r2", Number: 44, Status: store.ReleaseStatusFailed},
	}
	got := diagnoseResult(st)
	if got["cause"] != "release #44 failed health" {
		t.Fatalf("cause=%v", got["cause"])
	}
	if got["traffic"] != "still on #43" {
		t.Fatalf("traffic=%v", got["traffic"])
	}
	if got["recommendation"] != "rollback not required" {
		t.Fatalf("rec=%v", got["recommendation"])
	}
}
