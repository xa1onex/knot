package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestWorkflowDiagnoseDeployRestore(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	port1 := freePort(t)
	port2 := freePort(t)
	host := "wf.example.com"

	r1, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: port1, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, err = cl.DeployRelease(ctx, r1.ID, "", 0)
	if err != nil || r1.Status != "active" {
		t.Fatalf("r1: %+v err=%v", r1, err)
	}
	if _, err := cl.SwitchRouteTraffic(ctx, host, r1.ID, 100); err != nil {
		t.Fatal(err)
	}

	bad, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v2-unhealthy", DeviceID: idHome, Port: port2,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err = cl.DeployRelease(ctx, bad.ID, "", 0)
	if err != nil || bad.Status != "failed" {
		t.Fatalf("failed candidate: %+v err=%v", bad, err)
	}

	diag, err := cl.RunWorkflow(ctx, client.RunWorkflowRequest{Name: "diagnose-service", Service: "web-app"})
	if err != nil {
		t.Fatal(err)
	}
	if diag.Status != "succeeded" {
		t.Fatalf("diagnose status=%s err=%s steps=%+v", diag.Status, diag.Error, diag.Steps)
	}
	if diag.TraceID == "" {
		t.Fatal("missing workflow trace")
	}
	for _, st := range diag.Steps {
		if st.TraceID != diag.TraceID {
			t.Fatalf("step %s trace=%s want %s", st.Name, st.TraceID, diag.TraceID)
		}
		if st.Name != "health.check" && st.Status != "succeeded" {
			t.Fatalf("step %s status=%s", st.Name, st.Status)
		}
	}
	if rec, _ := diag.Result["recommendation"].(string); rec != "rollback not required" {
		t.Fatalf("result=%v", diag.Result)
	}
	if cause, _ := diag.Result["cause"].(string); !strings.Contains(cause, "failed health") {
		t.Fatalf("cause=%q", cause)
	}
	if got, err := cl.GetWorkflow(ctx, diag.ID); err != nil || got.Status != "succeeded" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	steps, err := cl.WorkflowSteps(ctx, diag.ID)
	if err != nil || len(steps) < 4 {
		t.Fatalf("steps: %v %+v", err, steps)
	}

	_, tokDiag, err := cl.CreateCredential(ctx, "diag", []string{
		permissions.ReleaseRead, permissions.TrafficRead, permissions.LogsRead,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	diagCl := client.New(ts.URL, tokDiag)
	ok, err := diagCl.RunWorkflow(ctx, client.RunWorkflowRequest{Name: "diagnose-service", Service: "web-app"})
	if err != nil || ok.Status != "succeeded" {
		t.Fatalf("diagnostic diagnose: %+v err=%v", ok, err)
	}
	for _, st := range ok.Steps {
		if st.Name == "health.check" && st.Status != "skipped" {
			t.Fatalf("health without services.read should skip: %+v", st)
		}
	}

	denied, err := diagCl.RunWorkflow(ctx, client.RunWorkflowRequest{
		Name: "deploy-release", Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: freePort(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if denied.Status != "denied" {
		t.Fatalf("deploy without write scopes: %+v", denied)
	}
	sawDenied, sawPending := false, false
	for _, st := range denied.Steps {
		if st.Status == "denied" {
			sawDenied = true
		}
		if st.Name == "deploy" && st.Status == "pending" {
			sawPending = true
		}
	}
	if !sawDenied || !sawPending {
		t.Fatalf("expected denied step and pending later steps: %+v", denied.Steps)
	}

	port3 := freePort(t)
	dep, err := cl.RunWorkflow(ctx, client.RunWorkflowRequest{
		Name: "deploy-release", Service: "web-app", Image: "knot-fake:v1",
		DeviceID: idHome, Port: port3, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dep.Status != "succeeded" {
		t.Fatalf("deploy-release: %+v", dep)
	}
	if dep.Result["traffic_switched"] == true {
		t.Fatalf("safe mode must not switch traffic: %v", dep.Result)
	}
	tr, err := cl.GetRouteTraffic(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if tr.ActiveReleaseID != r1.ID {
		t.Fatalf("production traffic moved to %s want %s", tr.ActiveReleaseID, r1.ID)
	}

	fail, err := cl.RunWorkflow(ctx, client.RunWorkflowRequest{Name: "deploy-release", Service: "web-app"})
	if err != nil {
		t.Fatal(err)
	}
	if fail.Status != "failed" {
		t.Fatalf("missing image should fail: %+v", fail)
	}
	pendingAfterFail := false
	failedStep := false
	for _, st := range fail.Steps {
		if st.Status == "failed" {
			failedStep = true
		}
		if st.Name == "deploy" && st.Status == "pending" {
			pendingAfterFail = true
		}
	}
	if !failedStep || !pendingAfterFail {
		t.Fatalf("error must stop remaining steps: %+v", fail.Steps)
	}

	putFile(t, cl, idHome, "backups/site.zip", []byte("PK\x03\x04 site-backup"))
	if _, err := cl.FilesReindex(ctx, idHome); err != nil {
		t.Fatal(err)
	}
	rest, err := cl.RunWorkflow(ctx, client.RunWorkflowRequest{
		Name: "restore-backup", Query: "backup", DeviceID: idHome, JobImage: "python:3.13",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rest.Status != "succeeded" {
		t.Fatalf("restore: %+v", rest)
	}
	foundSearch, foundJob := false, false
	for _, st := range rest.Steps {
		if st.Name == "files.search" && st.Status == "succeeded" {
			foundSearch = true
		}
		if st.Name == "jobs.create" && st.Status == "succeeded" {
			foundJob = true
		}
	}
	if !foundSearch || !foundJob {
		t.Fatalf("restore steps: %+v", rest.Steps)
	}

	list, err := cl.ListWorkflows(ctx)
	if err != nil || len(list.Catalog) != 3 || len(list.Workflows) < 3 {
		t.Fatalf("list: %+v err=%v", list, err)
	}

	events, err := cl.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run, step := false, false
	for _, e := range events {
		if e.Action == "workflow.run" {
			run = true
		}
		if e.Action == "workflow.step" {
			step = true
		}
	}
	if !run || !step {
		t.Fatalf("audit missing workflow actor/steps: %+v", events)
	}
}
