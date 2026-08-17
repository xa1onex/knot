package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestMCPAudit(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	port := freePort(t)
	host := "audit.example.com"
	rel, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: port, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	rel, err = cl.DeployRelease(ctx, rel.ID, "", 0)
	if err != nil || rel.Status != "active" {
		t.Fatalf("deploy: %+v err=%v", rel, err)
	}

	sess, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{
		Name: "AI-debug-001",
		Scopes: []string{
			permissions.DevicesRead, permissions.LogsRead, permissions.ReleaseRead,
			permissions.TrafficRead, permissions.TrafficWrite, permissions.ServicesRead,
			permissions.DeployRead,
		},
		TTLMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	ai := client.New(ts.URL, sess.Token)
	if _, err := ai.SearchAudit(ctx, client.AuditQuery{}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("AI must not read audit without audit.read: %v", err)
	}

	srv := &mcp.Server{Client: ai, MCPClient: "cursor-test"}
	if _, err := srv.Call(ctx, mcp.ToolAuditSearch, map[string]any{}); err == nil {
		t.Fatal("MCP audit.search must require audit.read")
	}
	if _, err := srv.Call(ctx, mcp.ToolDevicesList, nil); err != nil {
		t.Fatalf("devices.list: %v", err)
	}

	diagOut, err := srv.Call(ctx, mcp.ToolWorkflowRun, map[string]any{
		"name": "diagnose-service", "service": "web-app",
	})
	if err != nil {
		t.Fatalf("workflow.run: %v", err)
	}
	diag, ok := diagOut.(*client.Workflow)
	if !ok || diag.ID == "" || diag.TraceID == "" {
		t.Fatalf("workflow: %T %+v", diagOut, diagOut)
	}

	if _, err := srv.Call(ctx, mcp.ToolTrafficSwitch, map[string]any{
		"hostname": host, "release_id": rel.ID, "weight": 100,
	}); err != nil {
		t.Fatalf("traffic.switch: %v", err)
	}

	events, err := cl.SearchAudit(ctx, client.AuditQuery{ActorType: "ai_session", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	var sawDevice, sawWF, sawSwitch, sawMCP, sawParent bool
	var switchEv client.AuditEvent
	var deviceTrace string
	for _, e := range events {
		if e.ActorType != "ai_session" {
			t.Fatalf("user action mixed into AI filter: %+v", e)
		}
		if e.Actor != "AI-debug-001" {
			t.Fatalf("actor display: %+v", e)
		}
		if strings.Contains(e.Parent, "admin@") {
			sawParent = true
		}
		if e.MCPClient == "cursor-test" {
			sawMCP = true
		}
		if e.Action == "devices.list" && e.TraceID != "" {
			sawDevice = true
			deviceTrace = e.TraceID
		}
		if e.Action == "workflow.run" && e.WorkflowID == diag.ID && e.TraceID == diag.TraceID {
			sawWF = true
		}
		if e.Action == "traffic.switch" && e.Result == "SUCCESS" {
			sawSwitch = true
			switchEv = e
		}
	}
	if !sawDevice || !sawWF || !sawSwitch || !sawMCP || !sawParent {
		t.Fatalf("missing AI audit fields device=%v wf=%v switch=%v mcp=%v parent=%v events=%+v",
			sawDevice, sawWF, sawSwitch, sawMCP, sawParent, events)
	}
	if switchEv.Route != host {
		t.Fatalf("who switched: %+v", switchEv)
	}
	if switchEv.Release != rel.ID {
		t.Fatalf("release in audit: %+v", switchEv)
	}

	bySess, err := cl.SearchAudit(ctx, client.AuditQuery{AISessionID: sess.ID, Limit: 200})
	if err != nil || len(bySess) == 0 {
		t.Fatalf("session filter: %v %+v", err, bySess)
	}

	traced, err := cl.AuditTrace(ctx, deviceTrace)
	if err != nil || len(traced) == 0 {
		t.Fatalf("trace: %v %+v", err, traced)
	}
	for _, e := range traced {
		if e.TraceID != deviceTrace {
			t.Fatalf("trace leak: %+v", e)
		}
	}

	acts, err := cl.AIActivity(ctx, client.AuditQuery{AISessionID: sess.ID, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	foundDiag := false
	for _, a := range acts {
		if a.Ran == "diagnose-service" && a.Service == "web-app" && a.WorkflowID == diag.ID {
			foundDiag = true
			if len(a.Steps) == 0 {
				t.Fatalf("activity missing steps: %+v", a)
			}
		}
	}
	if !foundDiag {
		t.Fatalf("AI activity missing diagnose: %+v", acts)
	}

	users, err := cl.SearchAudit(ctx, client.AuditQuery{ActorType: "user", Action: "releases.create", Limit: 20})
	if err != nil || len(users) == 0 {
		t.Fatalf("user actions: %v %+v", err, users)
	}
	for _, e := range users {
		if e.ActorType == "ai_session" {
			t.Fatal("AI mixed into user filter")
		}
	}

	if err := cl.RevokeAISession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ai.ListDevices(ctx); err == nil {
		t.Fatal("revoked session still works")
	}
	after, err := cl.SearchAudit(ctx, client.AuditQuery{AISessionID: sess.ID, Limit: 200})
	if err != nil || len(after) < len(bySess) {
		t.Fatalf("revoke must not erase history: before=%d after=%d err=%v", len(bySess), len(after), err)
	}

	_, tokAdminOnly, err := cl.CreateCredential(ctx, "admin-only", []string{permissions.AccountAdmin}, 1)
	if err != nil {
		t.Fatal(err)
	}
	adm := client.New(ts.URL, tokAdminOnly)
	if _, err := adm.SearchAudit(ctx, client.AuditQuery{}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("account.admin must not imply audit.read: %v", err)
	}
}
