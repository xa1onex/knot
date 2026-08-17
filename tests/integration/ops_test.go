package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestOpsContextSnapshot(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	port1 := freePort(t)
	port2 := freePort(t)
	host := "ops.example.com"

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

	view, err := cl.OpsContext(ctx, "web-app", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Service != "web-app" || view.CurrentRelease == nil || view.CurrentRelease.ID != r1.ID {
		t.Fatalf("current: %+v", view)
	}
	if view.Traffic == nil || view.Traffic.Weight != 100 || view.Traffic.ActiveReleaseID != r1.ID {
		t.Fatalf("traffic: %+v", view.Traffic)
	}
	if view.TraceID == "" || view.Summary == "" {
		t.Fatalf("missing trace/summary: %+v", view)
	}
	if view.LastDeploy == nil {
		t.Fatalf("last deploy missing: %+v", view)
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

	view, err = cl.OpsContext(ctx, "web-app", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.CurrentRelease == nil || view.CurrentRelease.ID != r1.ID {
		t.Fatalf("production must stay r1: %+v", view.CurrentRelease)
	}
	if view.LatestRelease == nil || view.LatestRelease.ID != bad.ID || view.LatestRelease.Status != "failed" {
		t.Fatalf("latest should be failed candidate: %+v", view.LatestRelease)
	}
	if view.Status != "degraded" {
		t.Fatalf("status=%s summary=%s", view.Status, view.Summary)
	}
	if view.Traffic == nil || view.Traffic.ActiveReleaseID != r1.ID {
		t.Fatalf("traffic must remain on r1: %+v", view.Traffic)
	}

	_, tokDeploy, err := cl.CreateCredential(ctx, "deploy-only", []string{permissions.DeployWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	depCl := client.New(ts.URL, tokDeploy)
	if _, err := depCl.OpsContext(ctx, "web-app", ""); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy.write must not read ops context: %v", err)
	}

	_, tokDiag, err := cl.CreateCredential(ctx, "diag", []string{
		permissions.ReleaseRead, permissions.TrafficRead, permissions.LogsRead,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	diagCl := client.New(ts.URL, tokDiag)
	diag, err := diagCl.OpsContext(ctx, "web-app", "")
	if err != nil {
		t.Fatal(err)
	}
	if diag.CurrentRelease == nil || diag.Traffic == nil {
		t.Fatalf("diagnostic credential should see release+traffic: %+v", diag)
	}
	hasService := false
	for _, v := range diag.Visible {
		if v == "service" || v == "health" {
			hasService = true
		}
	}
	if hasService {
		t.Fatalf("diagnostic token must not imply services.read: %v", diag.Visible)
	}
}
