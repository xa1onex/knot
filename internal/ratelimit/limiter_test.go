package ratelimit_test

import (
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/ratelimit"
)

func TestLimiter(t *testing.T) {
	l := ratelimit.New(2, time.Minute)
	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("first two should pass")
	}
	if l.Allow("a") {
		t.Fatal("third should deny")
	}
	if !l.Allow("b") {
		t.Fatal("other key should pass")
	}
}
