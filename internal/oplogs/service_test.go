package oplogs_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
)

func TestRedactSecrets(t *testing.T) {
	got := oplogs.Redact("boot PASSWORD=hunter2 TOKEN=abc SECRET=xyz")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "abc") || strings.Contains(got, "xyz") {
		t.Fatalf("secret leaked: %q", got)
	}
	if !strings.Contains(got, "PASSWORD=[redacted]") {
		t.Fatalf("expected redacted password: %q", got)
	}
}

func TestEmitQuerySubscribePurge(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/knot.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := oplogs.New(st, 30)
	ctx := context.Background()
	user := "user-1"

	ch, cancel := svc.Subscribe(user, oplogs.Query{Source: oplogs.SourceDeploy})
	defer cancel()

	ev := svc.Emit(ctx, oplogs.Event{
		UserID: user, Source: oplogs.SourceDeploy, Message: "container started",
		TraceID: "abc123", Service: "web-app", ReleaseID: "rel-1",
	})
	if ev.ID == "" {
		t.Fatal("expected id")
	}
	select {
	case got := <-ch:
		if got.Message != "container started" || got.TraceID != "abc123" {
			t.Fatalf("subscribe: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscribe timeout")
	}

	list, err := svc.Query(ctx, user, oplogs.Query{Service: "web-app", TraceID: "abc123"})
	if err != nil || len(list) != 1 {
		t.Fatalf("query: %v n=%d", err, len(list))
	}

	old := &store.OpsLog{
		UserID: user, CreatedAt: time.Now().UTC().Add(-40 * 24 * time.Hour),
		Level: "info", Source: oplogs.SourceSystem, Message: "ancient",
	}
	if err := st.InsertOpsLog(ctx, old); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Purge(ctx)
	if err != nil || n < 1 {
		t.Fatalf("purge: n=%d err=%v", n, err)
	}
	list, err = svc.Query(ctx, user, oplogs.Query{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Message == "ancient" {
			t.Fatal("retention left old row")
		}
	}
}
