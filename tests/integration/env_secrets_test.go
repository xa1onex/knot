package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestEnvironmentsAndSecretsVault(t *testing.T) {
	dbPath := t.TempDir() + "/knot.db"
	ts, cl, _, _, _ := startCPFull(t, true, dbPath)
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	plainV1 := "vault-plain-9.1-UNIQUE-alpha"
	plainV2 := "vault-plain-9.1-UNIQUE-beta"

	sec, err := cl.CreateSecret(ctx, "DATABASE_URL", plainV1)
	if err != nil {
		t.Fatal(err)
	}
	if sec.Version != 1 || sec.ID == "" {
		t.Fatalf("secret: %+v", sec)
	}
	got, err := cl.GetSecret(ctx, sec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), plainV1) {
		t.Fatalf("secret value leaked in API: %+v", got)
	}

	rawDB, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawDB, []byte(plainV1)) {
		t.Fatal("plaintext secret stored in database")
	}

	staging, err := cl.CreateEnvironment(ctx, client.CreateEnvironmentRequest{
		Project: "web-app", Name: "staging",
		Vars: map[string]string{"NODE_ENV": "staging"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prod, err := cl.CreateEnvironment(ctx, client.CreateEnvironmentRequest{
		Project: "web-app", Name: "production",
		Vars:    map[string]string{"NODE_ENV": "production"},
		Secrets: map[string]string{"DATABASE_URL": sec.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prod.Secrets) != 1 || prod.Secrets[0].Version != 1 {
		t.Fatalf("prod secrets %+v", prod.Secrets)
	}

	port := freePort(t)
	v1, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: "knot-fake:v1", Port: port,
		Environment: "production", Project: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v1.Active || v1.EnvironmentID != prod.ID {
		t.Fatalf("v1 env: %+v", v1)
	}
	if v1.Env["NODE_ENV"] != "production" {
		t.Fatalf("prod vars not applied: %+v", v1.Env)
	}
	if v1.Env["DATABASE_URL"] != "" {
		t.Fatalf("secret value in deploy env: %+v", v1.Env)
	}
	if v1.Secrets["DATABASE_URL"].Version != 1 {
		t.Fatalf("pin %+v", v1.Secrets)
	}

	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV1 {
		t.Fatalf("container secret v1: %q", body)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/NODE_ENV", port)); body != "production" {
		t.Fatalf("container NODE_ENV: %q", body)
	}

	stgPort := freePort(t)
	stg, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app-staging", Image: "knot-fake:v1", Port: stgPort,
		Environment: "staging", Project: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stg.Env["NODE_ENV"] != "staging" {
		t.Fatalf("staging vars: %+v", stg.Env)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/NODE_ENV", stgPort)); body != "staging" {
		t.Fatalf("staging container: %q", body)
	}

	logs, err := cl.DeploymentLogs(ctx, v1.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l.Message, plainV1) {
			t.Fatalf("secret in logs: %q", l.Message)
		}
	}
	events, err := cl.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if strings.Contains(e.Detail, plainV1) || strings.Contains(e.Resource, plainV1) {
			t.Fatalf("secret in audit: %+v", e)
		}
	}

	rotated, err := cl.RotateSecret(ctx, sec.ID, plainV2)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Version != 2 {
		t.Fatalf("rotate %+v", rotated)
	}

	v2, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: idHome, Name: "web-app", Image: "knot-fake:v2", Port: port,
		Environment: "production", Project: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Image != "knot-fake:v2" || v2.Secrets["DATABASE_URL"].Version != 2 {
		t.Fatalf("v2 %+v", v2)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV2 {
		t.Fatalf("container secret v2: %q", body)
	}

	rolled, err := cl.RollbackDeployment(ctx, v2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Image != "knot-fake:v1" {
		t.Fatalf("rollback image %+v", rolled)
	}
	if rolled.Secrets["DATABASE_URL"].Version != 1 {
		t.Fatalf("rollback must pin secret v1: %+v", rolled.Secrets)
	}
	if body := httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/env/DATABASE_URL", port)); body != plainV1 {
		t.Fatalf("rollback container secret: %q", body)
	}

	_, tokDeploy, err := cl.CreateCredential(ctx, "deploy-only", []string{permissions.DeployRead, permissions.DeployWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	depCl := client.New(ts.URL, tokDeploy)
	if _, err := depCl.GetSecret(ctx, sec.ID); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy must not read secrets: %v", err)
	}
	if _, err := depCl.CreateSecret(ctx, "OTHER", "x"); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy must not write secrets: %v", err)
	}
	if _, err := depCl.GetDeployment(ctx, rolled.ID); err != nil {
		t.Fatalf("deploy.read should get deployment: %v", err)
	}

	_, tokRead, err := cl.CreateCredential(ctx, "secrets-read", []string{permissions.SecretsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tokRead)
	meta, err := ro.GetSecret(ctx, sec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v", meta), plainV1) || strings.Contains(fmt.Sprintf("%+v", meta), plainV2) {
		t.Fatalf("secrets.read returned value: %+v", meta)
	}
	if _, err := ro.CreateSecret(ctx, "NOPE", "x"); err == nil || !client.IsForbidden(err) {
		t.Fatalf("secrets.read cannot write: %v", err)
	}

	_ = staging
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
