package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestGitBuildToDeploy(t *testing.T) {
	dbPath := t.TempDir() + "/knot.db"
	ts, cl, _, _, _ := startCPFull(t, true, dbPath)
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS #3", t.TempDir())
	defer stopVPS()
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	src, err := cl.CreateSource(ctx, client.CreateSourceRequest{
		URL: "knot-fake-git:ok", Branch: "main", Name: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if src.CredentialSecretID != "" {
		t.Fatalf("source must not store credentials: %+v", src)
	}

	b1, err := cl.CreateBuild(ctx, client.CreateBuildRequest{
		SourceID: src.ID, DeviceID: idHome, Tag: "knot-fake:v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b1, err = cl.WaitBuild(waitCtx, b1.ID, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Status != "completed" || b1.Image != "knot-fake:v1" {
		t.Fatalf("build v1: %+v", b1)
	}
	logs, err := cl.BuildLogs(ctx, b1.ID, 50)
	if err != nil || len(logs) == 0 {
		t.Fatalf("build logs: %v len=%d", err, len(logs))
	}

	_, err = cl.CreateEnvironment(ctx, client.CreateEnvironmentRequest{
		Project: "web-app", Name: "production",
		Vars: map[string]string{"NODE_ENV": "production"},
	})
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	v1, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: b1.Image, Port: port,
		Environment: "production", Project: "web-app",
		Hostname: "build.example.com", EdgeDeviceID: idVPS,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := edgeGET(t, ts.Client(), ts.URL, "build.example.com", "/health")
	if got.Status != http.StatusOK || got.Body != "v1" {
		t.Fatalf("edge v1: status=%d body=%q", got.Status, got.Body)
	}

	b2, err := cl.CreateBuild(ctx, client.CreateBuildRequest{
		SourceID: src.ID, DeviceID: idHome, Tag: "knot-fake:v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	wait2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	b2, err = cl.WaitBuild(wait2, b2.ID, 20*time.Millisecond)
	if err != nil || b2.Status != "completed" {
		t.Fatalf("build v2: %+v err=%v", b2, err)
	}
	v2, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: b2.Image, Port: port,
		Environment: "production", Project: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	got2 := edgeGET(t, ts.Client(), ts.URL, "build.example.com", "/health")
	if got2.Status != http.StatusOK || got2.Body != "v2" {
		t.Fatalf("edge v2: status=%d body=%q", got2.Status, got2.Body)
	}

	rolled, err := cl.RollbackDeployment(ctx, v2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Image != "knot-fake:v1" {
		t.Fatalf("rollback image %+v", rolled)
	}
	got3 := edgeGET(t, ts.Client(), ts.URL, "build.example.com", "/health")
	if got3.Status != http.StatusOK || got3.Body != "v1" {
		t.Fatalf("rollback edge: status=%d body=%q", got3.Status, got3.Body)
	}
	_ = v1
}

func TestBuildFailedDockerfile(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()
	src, err := cl.CreateSource(ctx, client.CreateSourceRequest{URL: "knot-fake-git:bad-dockerfile", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := cl.CreateBuild(ctx, client.CreateBuildRequest{SourceID: src.ID, DeviceID: idHome, Tag: "app:broken"})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err = cl.WaitBuild(waitCtx, b.ID, 20*time.Millisecond)
	if err != nil || b.Status != "failed_build" {
		t.Fatalf("expected failed_build got %+v err=%v", b, err)
	}
}

func TestPrivateGitVaultSecretRedacted(t *testing.T) {
	dbPath := t.TempDir() + "/knot.db"
	ts, cl, _, _, _ := startCPFull(t, true, dbPath)
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()
	token := "git-secret-9.2-UNIQUE-token"

	srcNo, err := cl.CreateSource(ctx, client.CreateSourceRequest{URL: "knot-fake-git:private", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	fail, err := cl.CreateBuild(ctx, client.CreateBuildRequest{SourceID: srcNo.ID, DeviceID: idHome, Tag: "app:priv"})
	if err != nil {
		t.Fatal(err)
	}
	wait1, cancel1 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel1()
	fail, err = cl.WaitBuild(wait1, fail.ID, 20*time.Millisecond)
	if err != nil || fail.Status != "failed_clone" {
		t.Fatalf("expected failed_clone without token: %+v err=%v", fail, err)
	}

	sec, err := cl.CreateSecret(ctx, "git-token", token)
	if err != nil {
		t.Fatal(err)
	}
	src, err := cl.CreateSource(ctx, client.CreateSourceRequest{
		URL: "knot-fake-git:private", Branch: "main", CredentialSecretID: "secret://git-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if src.CredentialSecretID != sec.ID {
		t.Fatalf("expected secret id stored, got %+v", src)
	}
	if strings.Contains(fmt.Sprintf("%+v", src), token) {
		t.Fatalf("token in source object: %+v", src)
	}

	ok, err := cl.CreateBuild(ctx, client.CreateBuildRequest{SourceID: src.ID, DeviceID: idHome, Tag: "knot-fake:v1"})
	if err != nil {
		t.Fatal(err)
	}
	wait2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	ok, err = cl.WaitBuild(wait2, ok.ID, 20*time.Millisecond)
	if err != nil || ok.Status != "completed" {
		t.Fatalf("private git with vault: %+v err=%v", ok, err)
	}
	logs, err := cl.BuildLogs(ctx, ok.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l.Message, token) {
			t.Fatalf("token in logs: %q", l.Message)
		}
	}
	rawDB, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawDB, []byte(token)) {
		t.Fatal("git token stored in database")
	}

	events, err := cl.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		blob := fmt.Sprintf("%+v", e)
		if strings.Contains(blob, token) {
			t.Fatalf("token in audit: %+v", e)
		}
	}
}

func TestBuildNodeDisconnectFails(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()
	src, err := cl.CreateSource(ctx, client.CreateSourceRequest{URL: "knot-fake-git:hang", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := cl.CreateBuild(ctx, client.CreateBuildRequest{SourceID: src.ID, DeviceID: idHome, Tag: "app:hang"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cur, err := cl.GetBuild(ctx, b.ID)
		if err == nil && (cur.Status == "cloning" || cur.Status == "queued" || cur.Status == "building") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopHome()
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done, err := cl.WaitBuild(waitCtx, b.ID, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "failed" {
		t.Fatalf("disconnect expected failed, got %+v", done)
	}
	if !strings.Contains(done.Error, "disconnected") {
		t.Fatalf("error %q", done.Error)
	}
}

func TestBuildWriteDoesNotDeploy(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	_, tokBuild, err := cl.CreateCredential(ctx, "build-only", []string{permissions.SourceRead, permissions.SourceWrite, permissions.BuildRead, permissions.BuildWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	buildCl := client.New(ts.URL, tokBuild)
	src, err := buildCl.CreateSource(ctx, client.CreateSourceRequest{URL: "knot-fake-git:ok", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildCl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: "knot-fake:v1", Port: 3000,
	}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("build.write must not deploy: %v", err)
	}

	_, tokDeploy, err := cl.CreateCredential(ctx, "deploy-only", []string{permissions.DeployRead, permissions.DeployWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	depCl := client.New(ts.URL, tokDeploy)
	if _, err := depCl.CreateBuild(ctx, client.CreateBuildRequest{SourceID: src.ID, DeviceID: idHome, Tag: "app:x"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy.write must not build: %v", err)
	}
}
