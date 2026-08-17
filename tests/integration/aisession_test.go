package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestAISessionScopedCredential(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()

	sess, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{
		Name: "AI-debug-001",
		Scopes: []string{
			permissions.LogsRead, permissions.ReleaseRead, permissions.TrafficRead, permissions.DevicesRead,
		},
		TTLMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token == "" || !strings.HasPrefix(sess.Token, "knot_ai_") {
		t.Fatalf("token shown once, got %q", sess.Token)
	}
	if sess.Status != "active" || sess.Parent == "" {
		t.Fatalf("session: %+v", sess)
	}

	listed, err := cl.ListAISessions(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %+v err=%v", listed, err)
	}
	if listed[0].Token != "" {
		t.Fatal("list must not return the secret")
	}

	ai := client.New(ts.URL, sess.Token)
	cur, err := ai.CurrentAISession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Token != "" || cur.Status != "active" {
		t.Fatalf("current leaked token or bad status: %+v", cur)
	}
	if !strings.Contains(cur.Parent, "@") && cur.CreatedBy == "" {
		t.Fatalf("missing parent: %+v", cur)
	}

	if _, err := ai.ListDevices(ctx); err != nil {
		t.Fatalf("scoped token should list devices: %v", err)
	}
	if _, err := ai.ListLogs(ctx, client.ListLogsQuery{Limit: 5}); err != nil {
		t.Fatalf("logs.read should work: %v", err)
	}
	if _, err := ai.SwitchRouteTraffic(ctx, "missing.example", "rel", 100); err == nil || !client.IsForbidden(err) {
		t.Fatalf("traffic.write must not be implied: %v", err)
	}

	_, err = ai.CreateAISession(ctx, client.CreateAISessionRequest{
		Scopes: []string{permissions.LogsRead}, ExpiresIn: "30m",
	})
	if err == nil || !client.IsForbidden(err) {
		t.Fatalf("AI must not mint sessions: %v", err)
	}

	_, tokLimited, err := cl.CreateCredential(ctx, "limited", []string{permissions.CredentialsRW, permissions.LogsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	lim := client.New(ts.URL, tokLimited)
	_, err = lim.CreateAISession(ctx, client.CreateAISessionRequest{
		Scopes: []string{permissions.TrafficWrite}, TTLMinutes: 30,
	})
	if err == nil || !client.IsValidation(err) {
		t.Fatalf("cannot expand rights: %v", err)
	}

	srv := &mcp.Server{Client: ai}
	out, err := srv.Call(ctx, mcp.ToolAISession, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(*client.AISession)
	if !ok || got.ID != sess.ID {
		t.Fatalf("mcp ai.session: %T %+v", out, out)
	}
	if _, err := srv.Call(ctx, mcp.ToolDevicesList, nil); err != nil {
		t.Fatalf("mcp devices.list with AI token: %v", err)
	}

	events, err := cl.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.ActorType == "ai_session" && strings.Contains(e.ParentActor, "@") {
			found = true
			break
		}
		if strings.Contains(e.Actor, "ai-session:") && strings.Contains(e.Actor, "parent:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("audit missing AI parent: %+v", events)
	}

	short, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{
		Name: "ttl", Scopes: []string{permissions.DevicesRead}, ExpiresIn: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	expCl := client.New(ts.URL, short.Token)
	_, err = expCl.ListDevices(ctx)
	if err == nil || !client.IsCode(err, apierrors.CodeTokenExpired) {
		t.Fatalf("expired must 401 token_expired: %v", err)
	}

	if err := cl.RevokeAISession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	_, err = ai.ListDevices(ctx)
	if err == nil || !client.IsCode(err, apierrors.CodeTokenRevoked) {
		t.Fatalf("revoke must 401 token_revoked: %v", err)
	}
}
