package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestPlanProposalAndApprovalGate(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	ctx := context.Background()

	sess, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{
		Name: "Deploy-Agent", Scopes: []string{permissions.LogsRead, permissions.ReleaseRead, permissions.DevicesRead}, TTLMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	ai := client.New(ts.URL, sess.Token)
	mcpSrv := &mcp.Server{Client: ai, MCPClient: "cursor-test"}

	before, err := cl.ListReleases(ctx, "web-app")
	if err != nil {
		t.Fatal(err)
	}

	out, err := mcpSrv.Call(ctx, mcp.ToolPlanCreate, map[string]any{
		"intent": "Обновить production", "service": "web-app", "image": "knot-fake:v1",
		"hostname": "plan.example.com", "device_id": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := out.(*client.Plan)
	if !ok || p.ID == "" {
		t.Fatalf("plan: %T %+v", out, out)
	}
	if p.RiskLevel != "critical" || p.Status != "ready_for_approval" {
		t.Fatalf("risk/status: %+v", p)
	}
	if len(p.Steps) < 5 || p.Steps[len(p.Steps)-1].Name != "traffic.switch" {
		t.Fatalf("steps=%+v", p.Steps)
	}
	after, _ := cl.ListReleases(ctx, "web-app")
	if len(before) != len(after) {
		t.Fatal("plan.create must not mutate releases")
	}

	if _, err := mcpSrv.Call(ctx, mcp.ToolPlanApprove, map[string]any{"id": p.ID}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("AI plan.approve must 403: %v", err)
	}
	if _, err := ai.ExecutePlan(ctx, p.ID); err == nil || !client.IsForbidden(err) {
		t.Fatalf("AI execute critical must 403: %v", err)
	}

	_, tokLimited, err := cl.CreateCredential(ctx, "weak-approver", []string{permissions.DevicesRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	weak := client.New(ts.URL, tokLimited)
	got, err := weak.ApprovePlan(ctx, p.ID)
	if err != nil && !client.IsForbidden(err) {
		t.Fatalf("weak approve: %v", err)
	}
	if err == nil && got.Status != "denied" {
		t.Fatalf("weak approver must fail a step: %+v", got)
	}

	other, err := ai.CreatePlan(ctx, client.CreatePlanRequest{Name: "update-production", Service: "web-app", Image: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := cl.CancelPlan(ctx, other.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	if _, err := cl.ApprovePlan(ctx, other.ID); err == nil {
		t.Fatal("cancelled plan approved")
	}

	expired, err := ai.CreatePlan(ctx, client.CreatePlanRequest{
		Name: "update-production", Service: "web-app", Image: "x", ExpiresIn: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := cl.ApprovePlan(ctx, expired.ID); err == nil {
		t.Fatal("expired plan approved")
	}

	diag, err := ai.CreatePlan(ctx, client.CreatePlanRequest{Name: "diagnose-service", Service: "web-app"})
	if err != nil {
		t.Fatal(err)
	}
	if diag.Status != "ready" || diag.RequiresApproval {
		t.Fatalf("diagnose: %+v", diag)
	}
	ran, err := ai.ExecutePlan(ctx, diag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ran.Status == "ready_for_approval" {
		t.Fatal("read plan should execute without approve")
	}
	events, err := cl.SearchAudit(ctx, client.AuditQuery{Action: "plan.step", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("plan steps must be audited")
	}
}

func TestPlanApproveExecutesProductionUpdate(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	port1 := freePort(t)
	port2 := freePort(t)
	host := "plan-exec.example.com"
	r1, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: port1, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, err = cl.DeployRelease(ctx, r1.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.SwitchRouteTraffic(ctx, host, r1.ID, 100); err != nil {
		t.Fatal(err)
	}

	sess, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{
		Name: "Deploy-Agent", Scopes: []string{permissions.LogsRead, permissions.ReleaseRead}, TTLMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	ai := client.New(ts.URL, sess.Token)
	p, err := ai.CreatePlan(ctx, client.CreatePlanRequest{
		Name: "update-production", Service: "web-app", Image: "knot-fake:v1",
		DeviceID: idHome, Port: port2, Hostname: host, Intent: "Обновить production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "ready_for_approval" {
		t.Fatalf("want proposal, got %+v", p)
	}

	done, err := cl.ApprovePlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "succeeded" {
		t.Fatalf("execute: status=%s err=%s steps=%+v", done.Status, done.Error, done.Steps)
	}
	if done.TraceID == "" {
		t.Fatal("missing plan trace")
	}
	var sawSwitch bool
	for _, st := range done.Steps {
		if st.TraceID == "" || st.TraceID == done.TraceID {
			t.Fatalf("each step needs its own trace: plan=%s step=%s", done.TraceID, st.TraceID)
		}
		if st.Name == "traffic.switch" && st.Status == "succeeded" {
			sawSwitch = true
		}
		if st.Status != "succeeded" {
			t.Fatalf("step %s status=%s err=%s", st.Name, st.Status, st.Error)
		}
	}
	if !sawSwitch {
		t.Fatal("traffic.switch missing")
	}
	events, err := cl.SearchAudit(ctx, client.AuditQuery{Action: "plan.step", Limit: 50})
	if err != nil || len(events) == 0 {
		t.Fatalf("audit steps: %v %+v", err, events)
	}
	tr, err := cl.GetRouteTraffic(ctx, host)
	if err != nil || tr.ActiveReleaseID == r1.ID {
		t.Fatalf("traffic should move off r1: %+v err=%v", tr, err)
	}
}
