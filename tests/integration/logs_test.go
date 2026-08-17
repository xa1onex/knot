package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestOpsLogsAggregation(t *testing.T) {
	ts, cl, st, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx := context.Background()

	src, err := cl.CreateSource(ctx, client.CreateSourceRequest{
		URL: "knot-fake-git:ok", Branch: "main", Name: "web-app",
	})
	if err != nil {
		t.Fatal(err)
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
	if b1.Status != "completed" || b1.TraceID == "" {
		t.Fatalf("build: %+v", b1)
	}
	buildLogs, err := cl.ListLogs(ctx, client.ListLogsQuery{Source: "build", BuildID: b1.ID})
	if err != nil || len(buildLogs) == 0 {
		t.Fatalf("build ops logs: %v n=%d", err, len(buildLogs))
	}

	host := "logs.example.com"
	port := freePort(t)
	rel, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
		Service: "web-app", BuildID: b1.ID, DeviceID: idHome, Port: port, Hostname: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel.TraceID != b1.TraceID {
		t.Fatalf("release trace %q != build %q", rel.TraceID, b1.TraceID)
	}
	rel, err = cl.DeployRelease(ctx, rel.ID, "", 0)
	if err != nil || rel.Status != "active" {
		t.Fatalf("deploy: %+v err=%v", rel, err)
	}
	if _, err := cl.SwitchRouteTraffic(ctx, host, rel.ID, 100); err != nil {
		t.Fatal(err)
	}

	svcLogs, err := cl.ListLogs(ctx, client.ListLogsQuery{Service: "web-app"})
	if err != nil {
		t.Fatal(err)
	}
	relLogs, err := cl.ListLogs(ctx, client.ListLogsQuery{ReleaseID: rel.ID})
	if err != nil || len(relLogs) == 0 {
		t.Fatalf("release logs: %v n=%d", err, len(relLogs))
	}
	traceLogs, err := cl.ListLogs(ctx, client.ListLogsQuery{TraceID: b1.TraceID})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, e := range traceLogs {
		sources[e.Source] = true
		if e.TraceID != b1.TraceID {
			t.Fatalf("trace mismatch: %+v", e)
		}
	}
	if !sources["build"] || !sources["release"] || !sources["deploy"] {
		t.Fatalf("trace chain sources=%v logs=%d service=%d", sources, len(traceLogs), len(svcLogs))
	}

	ingested, err := cl.IngestLog(ctx, client.IngestLogRequest{
		Source: "system", Service: "web-app", ReleaseID: rel.ID, TraceID: b1.TraceID,
		Message: "boot PASSWORD=hunter2 TOKEN=s3cret SECRET=hidden",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ingested.Message, "hunter2") || strings.Contains(ingested.Message, "s3cret") || strings.Contains(ingested.Message, "hidden") {
		t.Fatalf("secret leaked: %q", ingested.Message)
	}

	after := ""
	if len(svcLogs) > 0 {
		after = svcLogs[len(svcLogs)-1].ID
	}
	ping, err := cl.IngestLog(ctx, client.IngestLogRequest{
		Source: "system", Service: "web-app", Message: "live tail ping", TraceID: b1.TraceID, ReleaseID: rel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	tailed, err := cl.ListLogs(ctx, client.ListLogsQuery{Service: "web-app", After: after})
	if err != nil {
		t.Fatal(err)
	}
	foundTail := false
	for _, e := range tailed {
		if e.ID == ping.ID || strings.Contains(e.Message, "live tail ping") {
			foundTail = true
			break
		}
	}
	if !foundTail {
		t.Fatalf("live tail missed ping after=%s n=%d", after, len(tailed))
	}

	stopHome()
	time.Sleep(80 * time.Millisecond)
	got := edgeGET(t, ts.Client(), ts.URL, host, "/health")
	if got.Status != http.StatusServiceUnavailable && got.Status != http.StatusBadGateway {
		t.Fatalf("expected edge 502/503, got %d %q", got.Status, got.Body)
	}
	deadline := time.Now().Add(2 * time.Second)
	var edgeLogs []client.OpsLog
	for time.Now().Before(deadline) {
		edgeLogs, err = cl.ListLogs(ctx, client.ListLogsQuery{Source: "edge", Service: "web-app"})
		if err == nil && len(edgeLogs) > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(edgeLogs) == 0 {
		t.Fatal("expected edge error log")
	}
	if edgeLogs[len(edgeLogs)-1].TraceID != b1.TraceID {
		t.Fatalf("edge trace: %+v", edgeLogs[len(edgeLogs)-1])
	}

	me, err := cl.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertOpsLog(ctx, &store.OpsLog{
		UserID: me.UserID, CreatedAt: time.Now().UTC().Add(-40 * 24 * time.Hour),
		Level: "info", Source: "system", Message: "ancient-row",
	}); err != nil {
		t.Fatal(err)
	}
	n, err := oplogs.New(st, 30).Purge(ctx)
	if err != nil || n < 1 {
		t.Fatalf("retention purge: n=%d err=%v", n, err)
	}
	left, err := cl.ListLogs(ctx, client.ListLogsQuery{Q: "ancient-row"})
	if err != nil || len(left) != 0 {
		t.Fatalf("old log still present: %v n=%d", err, len(left))
	}

	_, tokDeploy, err := cl.CreateCredential(ctx, "deploy-only", []string{permissions.DeployRead, permissions.DeployWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	depCl := client.New(ts.URL, tokDeploy)
	if _, err := depCl.ListLogs(ctx, client.ListLogsQuery{}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("deploy.write must not read logs: %v", err)
	}
	_, tokLogs, err := cl.CreateCredential(ctx, "logs-only", []string{permissions.LogsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	logsCl := client.New(ts.URL, tokLogs)
	if _, err := logsCl.ListLogs(ctx, client.ListLogsQuery{Service: "web-app"}); err != nil {
		t.Fatalf("logs.read should list: %v", err)
	}
	if _, err := logsCl.IngestLog(ctx, client.IngestLogRequest{Source: "system", Message: "nope"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("logs.read must not write: %v", err)
	}
}
