package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestReleaseHealthGateAndRollback(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	plainV1 := "release-secret-9.3-UNIQUE-v1"
	plainV2 := "release-secret-9.3-UNIQUE-v2"
	sec, err := cl.CreateSecret(ctx, "DATABASE_URL", plainV1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cl.CreateEnvironment(ctx, client.CreateEnvironmentRequest{
		Project: "web-app", Name: "production",
		Vars:    map[string]string{"NODE_ENV": "production"},
		Secrets: map[string]string{"DATABASE_URL": sec.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	r1, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", Environment: "production", Project: "web-app",
		DeviceID: idHome, Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != "created" || r1.Number != 1 || r1.ConfigVersion == "" {
		t.Fatalf("create r1: %+v", r1)
	}
	if r1.Secrets["DATABASE_URL"].Version != 1 {
		t.Fatalf("pin at create: %+v", r1.Secrets)
	}

	if _, err := cl.RotateSecret(ctx, sec.ID, plainV2); err != nil {
		t.Fatal(err)
	}

	r1, err = cl.DeployRelease(ctx, r1.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != "active" || !r1.Current || r1.DeploymentID == "" {
		t.Fatalf("deploy r1: %+v", r1)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port)); body != "v1" {
		t.Fatalf("health r1: %q", body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV1 {
		t.Fatalf("secret r1: %q", body)
	}

	r2, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v2", Environment: "production", Project: "web-app",
		DeviceID: idHome, Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Number != 2 || r2.PrevReleaseID != r1.ID {
		t.Fatalf("history r2: %+v", r2)
	}
	if r2.Secrets["DATABASE_URL"].Version != 2 {
		t.Fatalf("r2 should pin rotated secret: %+v", r2.Secrets)
	}

	r2, err = cl.DeployRelease(ctx, r2.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != "active" || !r2.Current {
		t.Fatalf("deploy r2: %+v", r2)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port)); body != "v2" {
		t.Fatalf("health r2: %q", body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV2 {
		t.Fatalf("secret r2: %q", body)
	}

	hist, err := cl.ListReleases(ctx, "web-app")
	if err != nil || len(hist) < 2 {
		t.Fatalf("history: %v %+v", err, hist)
	}

	rolled, err := cl.RollbackRelease(ctx, r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Status != "rolled_back" || rolled.Current {
		t.Fatalf("rollback r2: %+v", rolled)
	}
	cur, err := cl.GetRelease(ctx, r1.ID)
	if err != nil || !cur.Current || cur.Status != "active" {
		t.Fatalf("restored r1: %+v err=%v", cur, err)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port)); body != "v1" {
		t.Fatalf("rollback health: %q", body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV1 {
		t.Fatalf("rollback must restore pinned v1: %q", body)
	}

	logs, err := cl.ReleaseLogs(ctx, r1.ID, 100)
	if err != nil || len(logs) == 0 {
		t.Fatalf("release logs: %v len=%d", err, len(logs))
	}
	var hasRelease, hasDeploy bool
	for _, l := range logs {
		if strings.Contains(l.Message, plainV1) || strings.Contains(l.Message, plainV2) {
			t.Fatalf("secret in release logs: %q", l.Message)
		}
		if l.Source == "release" {
			hasRelease = true
		}
		if l.Source == "deploy" {
			hasDeploy = true
		}
	}
	if !hasRelease || !hasDeploy {
		t.Fatalf("expected linked release+deploy logs: %+v", logs)
	}

	bad, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v2-unhealthy", Environment: "production", Project: "web-app",
		DeviceID: idHome, Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err = cl.DeployRelease(ctx, bad.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if bad.Status != "failed" {
		t.Fatalf("health fail expected failed: %+v", bad)
	}
	cur, err = cl.GetRelease(ctx, r1.ID)
	if err != nil || !cur.Current {
		t.Fatalf("auto-restore r1: %+v err=%v", cur, err)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/health", port)); body != "v1" {
		t.Fatalf("after failed gate: %q", body)
	}

	_, tokDeploy, err := cl.CreateCredential(ctx, "deploy-only", []string{permissions.DeployRead, permissions.DeployWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	depCl := client.New(ts.URL, tokDeploy)
	if _, err := depCl.CreateRelease(ctx, client.CreateReleaseRequest{Service: "web-app", Image: "knot-fake:v1"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy.write must not create release: %v", err)
	}

	_, tokRel, err := cl.CreateCredential(ctx, "release-write", []string{permissions.ReleaseRead, permissions.ReleaseWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	relCl := client.New(ts.URL, tokRel)
	made, err := relCl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", Image: "knot-fake:v1", DeviceID: idHome, Port: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relCl.RollbackRelease(ctx, r1.ID); err == nil || !client.IsForbidden(err) {
		t.Fatalf("release.write must not rollback: %v", err)
	}
	if _, err := relCl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "other", Image: "knot-fake:v1", Port: freePort(t),
	}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("release.write must not deploy.write: %v", err)
	}
	_ = made
}
