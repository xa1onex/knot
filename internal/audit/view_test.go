package audit

import (
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/store"
)

func TestEventViewTrafficSwitch(t *testing.T) {
	e := store.AuditEvent{
		Action:      "traffic.switch",
		ActorType:   store.ActorTypeAISession,
		Actor:       "Deploy-Agent",
		ParentActor: "admin@node.local",
		Resource:    "route-1",
		Detail:      "example.com → rel-44",
		Result:      "SUCCESS",
		CreatedAt:   time.Date(2026, 8, 17, 18, 12, 0, 0, time.UTC),
	}
	v := EventView(e)
	if v["action"] != "traffic.switch" || v["actor_type"] != "ai_session" {
		t.Fatalf("%v", v)
	}
	if v["actor"] != "Deploy-Agent" || v["parent"] != "admin@node.local" {
		t.Fatalf("actor/parent: %v", v)
	}
	if v["route"] != "example.com" || v["release"] != "rel-44" {
		t.Fatalf("route/release: %v", v)
	}
}

func TestBuildAIActivityGroupsWorkflow(t *testing.T) {
	now := time.Date(2026, 8, 17, 18, 2, 0, 0, time.UTC)
	wfID := "wf1"
	events := []store.AuditEvent{
		{Action: "workflow.finish", ActorType: store.ActorTypeAISession, Actor: "AI-debug-001", ParentActor: "admin@node.local", WorkflowID: wfID, Detail: "diagnose-service", Result: "SUCCESS", CreatedAt: now.Add(2 * time.Second)},
		{Action: "workflow.step", ActorType: store.ActorTypeAISession, Actor: "AI-debug-001", WorkflowID: wfID, Detail: "logs.search", Result: "SUCCESS", CreatedAt: now.Add(time.Second)},
		{Action: "workflow.run", ActorType: store.ActorTypeAISession, Actor: "AI-debug-001", ParentActor: "admin@node.local", WorkflowID: wfID, Detail: "diagnose-service", Result: "SUCCESS", CreatedAt: now, AISessionID: "sess"},
		{Action: "devices.list", ActorType: store.ActorTypeAISession, Actor: "AI-debug-001", Result: "SUCCESS", CreatedAt: now.Add(3 * time.Second)},
	}
	wfs := map[string]*store.Workflow{
		wfID: {ID: wfID, Name: "diagnose-service", InputJSON: `{"service":"web-app"}`, ResultJSON: `{"cause":"release #44 failed health"}`},
	}
	got := BuildAIActivity(events, wfs)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Action != "devices.list" {
		t.Fatalf("newest first: %+v", got[0])
	}
	wf := got[1]
	if wf.Ran != "diagnose-service" || wf.Service != "web-app" || wf.Result != "release #44 failed health" {
		t.Fatalf("workflow activity: %+v", wf)
	}
	if len(wf.Steps) != 1 || wf.Steps[0].Name != "logs.search" || !wf.Steps[0].OK {
		t.Fatalf("steps: %+v", wf.Steps)
	}
}
