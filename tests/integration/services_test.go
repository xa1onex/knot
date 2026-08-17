package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestServiceRegistryTree(t *testing.T) {
	ts, cl, _ := startCP(t, true)
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS #3", t.TempDir())
	defer stopHome()
	defer stopVPS()
	time.Sleep(80 * time.Millisecond)

	ctx := context.Background()
	web, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if web.Listen != "http://127.0.0.1:3000" || web.Status != "registered" {
		t.Fatalf("%+v", web)
	}
	if _, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "api", Kind: "api", Port: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "postgres", Kind: "database", Port: 5432,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idVPS, Name: "edge", Kind: "web", Port: 80,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "web-app", Kind: "web", Port: 3001,
	}); err == nil || !client.IsCode(err, "conflict") {
		t.Fatalf("expected conflict, got %v", err)
	}

	tree, err := cl.ServicesTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var home *client.ServiceNode
	for i := range tree {
		if tree[i].DeviceID == idHome {
			home = &tree[i]
		}
	}
	if home == nil {
		t.Fatalf("Home PC missing from tree: %+v", tree)
	}
	if home.DeviceName != "Home PC" {
		t.Fatalf("name=%q", home.DeviceName)
	}
	got := map[string]string{}
	for _, svc := range home.Services {
		got[svc.Name] = svc.Listen
	}
	if got["web-app"] != "http://127.0.0.1:3000" || got["api"] != "http://127.0.0.1:8080" || got["postgres"] != "tcp://127.0.0.1:5432" {
		t.Fatalf("tree %+v", home.Services)
	}

	listed, err := cl.ListServices(ctx, idHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("list home=%d", len(listed))
	}

	port := 3001
	upd, err := cl.UpdateService(ctx, web.ID, client.UpdateServiceRequest{Port: &port})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Listen != "http://127.0.0.1:3001" {
		t.Fatalf("update %+v", upd)
	}

	if err := cl.DeleteService(ctx, web.ID); err != nil {
		t.Fatal(err)
	}
	after, err := cl.ListServices(ctx, idHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range after {
		if svc.Name == "web-app" {
			t.Fatal("deleted service still listed")
		}
	}

	_, tok, err := cl.CreateCredential(ctx, "svc-ro", []string{permissions.ServicesRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tok)
	if _, err := ro.ServicesTree(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ro.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: idHome, Name: "blocked", Port: 9,
	}); err == nil || !client.IsCode(err, "forbidden") {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
