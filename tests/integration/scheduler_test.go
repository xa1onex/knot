package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/agent/jobrunner"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
	"github.com/knot-infra/knot/pkg/protocol"
)

func gpuHomePolicy() jobrunner.Policy {
	p := jobrunner.TestPolicy()
	p.MaxCPU = 16
	p.MaxMemoryMB = 64 * 1024
	p.MaxGPU = 1
	p.GPURuntimeOK = true
	p.MaxDiskMB = 64 * 1024
	return p
}

func seedComputeSnapshot(t *testing.T, st *store.Store, deviceID string, cores int, ramBytes uint64, gpu []protocol.ComputeGPU, diskFree, diskTotal uint64) {
	t.Helper()
	inv := protocol.ComputeInventory{
		CPU:    protocol.ComputeCPU{Cores: cores, Architecture: "amd64"},
		Memory: protocol.ComputeMemory{TotalBytes: ramBytes, AvailableBytes: ramBytes / 2, UsedBytes: ramBytes / 2},
		Disks:  []protocol.ComputeDisk{{Mount: "/", TotalBytes: diskTotal, FreeBytes: diskFree, UsedBytes: diskTotal - diskFree}},
	}
	if gpu != nil {
		inv.GPU = &gpu
	}
	body, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDeviceCompute(context.Background(), deviceID, string(body), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func waitJobStatus(t *testing.T, cl *client.Client, ctx context.Context, id string, want ...string) *client.ComputeJob {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	var last *client.ComputeJob
	for time.Now().Before(deadline) {
		j, err := cl.GetJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		last = j
		for _, w := range want {
			if j.Status == w {
				return j
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx: %v last=%+v", ctx.Err(), last)
		case <-time.After(40 * time.Millisecond):
		}
	}
	t.Fatalf("want %v, got %+v", want, last)
	return last
}

func TestComputeSchedulerPlacesGPUJobOnHomePC(t *testing.T) {
	ts, cl, st, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnectWithJobPolicy(t, ts, cl, "Home PC", t.TempDir(), gpuHomePolicy())
	defer stopHome()
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS", t.TempDir())
	defer stopVPS()
	time.Sleep(80 * time.Millisecond)

	rtx := []protocol.ComputeGPU{{Vendor: "NVIDIA", Model: "RTX 4090", VRAMBytes: u64(12 << 30), Available: bptr(true)}}
	seedComputeSnapshot(t, st, idHome, 16, 64<<30, rtx, 400<<30, 4<<40)
	seedComputeSnapshot(t, st, idVPS, 4, 8<<30, nil, 20<<30, 80<<30)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	job, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:     "python:3.13",
		Resources: client.JobResources{CPU: 8, MemoryMB: 16384, GPU: 1, DiskMB: 20480},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Placement != "scheduled" {
		t.Fatalf("placement %+v", job)
	}
	got, err := cl.WaitJob(ctx, job.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "artifacts_committed" {
		t.Fatalf("want artifacts_committed, got %+v", got)
	}
	if got.DeviceID != idHome {
		t.Fatalf("scheduler must pick Home PC, got device %s want %s", got.DeviceID, idHome)
	}
	arts, err := cl.JobArtifacts(ctx, job.ID)
	if err != nil || len(arts) == 0 {
		t.Fatalf("artifacts: %v %+v", err, arts)
	}
}

func TestComputeSchedulerUnsatisfiableAndQueueAndLabels(t *testing.T) {
	ts, cl, st, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnectWithJobPolicy(t, ts, cl, "Home PC", t.TempDir(), gpuHomePolicy())
	defer stopHome()
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS", t.TempDir())
	defer stopVPS()
	time.Sleep(80 * time.Millisecond)

	rtx := []protocol.ComputeGPU{{Vendor: "NVIDIA", Model: "RTX 4090", Available: bptr(true)}}
	seedComputeSnapshot(t, st, idHome, 16, 64<<30, rtx, 400<<30, 4<<40)
	seedComputeSnapshot(t, st, idVPS, 4, 8<<30, nil, 20<<30, 80<<30)

	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()

	rej, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:     "python:3.13",
		Resources: client.JobResources{CPU: 32, MemoryMB: 16384, GPU: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rej.Status != "rejected" || rej.Reason != "unsatisfiable" {
		t.Fatalf("unsatisfiable: %+v", rej)
	}

	hang, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:          "knot-fake-job:hang",
		TimeoutSeconds: 60,
		Resources:      client.JobResources{CPU: 8, MemoryMB: 16384, GPU: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	running := waitJobStatus(t, cl, ctx, hang.ID, "running")
	if running.DeviceID != idHome {
		t.Fatalf("hang should be on Home, got %s", running.DeviceID)
	}

	queued, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:     "python:3.13",
		Resources: client.JobResources{CPU: 8, MemoryMB: 16384, GPU: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != "waiting_for_resource" {
		t.Fatalf("gpu busy should queue, got %+v", queued)
	}

	if _, err := cl.CancelJob(ctx, hang.ID); err != nil {
		t.Fatal(err)
	}
	hangGot, err := cl.WaitJob(ctx, hang.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if hangGot.Status != "canceled" {
		t.Fatalf("cancel hang: %+v", hangGot)
	}

	released, err := cl.WaitJob(ctx, queued.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != "artifacts_committed" || released.DeviceID != idHome {
		t.Fatalf("after GPU free: %+v", released)
	}

	if _, err := cl.SetComputeLabels(ctx, idHome, map[string]string{"location": "home", "trusted": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.SetComputeLabels(ctx, idVPS, map[string]string{"location": "cloud", "public": "true"}); err != nil {
		t.Fatal(err)
	}

	reqHome, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:     "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
		Require:   map[string]string{"gpu": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqGot, err := cl.WaitJob(ctx, reqHome.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if reqGot.DeviceID != idHome {
		t.Fatalf("require gpu=true → Home, got %s", reqGot.DeviceID)
	}

	pref, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:     "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
		Prefer:    map[string]string{"location": "cloud"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prefGot, err := cl.WaitJob(ctx, pref.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if prefGot.DeviceID != idVPS {
		t.Fatalf("prefer location=cloud → VPS, got %s", prefGot.DeviceID)
	}

	_, tok, err := cl.CreateCredential(ctx, "no-write", []string{permissions.ComputeRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tok)
	if _, err := ro.ListComputeDevices(ctx); err != nil {
		t.Fatalf("compute.read list: %v", err)
	}
	if _, err := ro.SetComputeLabels(ctx, idHome, map[string]string{"x": "1"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("labels need compute.write, got %v", err)
	}
}

func TestComputeSchedulerRetryElsewhereAfterNodeDeath(t *testing.T) {
	ts, cl, st, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnectWithJobPolicy(t, ts, cl, "Home PC", t.TempDir(), gpuHomePolicy())
	idVPS, stopVPS := registerAndConnect(t, ts, cl, "VPS", t.TempDir())
	defer stopVPS()
	time.Sleep(80 * time.Millisecond)

	seedComputeSnapshot(t, st, idHome, 16, 64<<30, nil, 400<<30, 4<<40)
	seedComputeSnapshot(t, st, idVPS, 4, 8<<30, nil, 20<<30, 80<<30)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	hang, err := cl.CreateJob(ctx, client.CreateJobRequest{
		Image:          "knot-fake-job:hang",
		TimeoutSeconds: 60,
		Resources:      client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	onHome := waitJobStatus(t, cl, ctx, hang.ID, "running")
	if onHome.DeviceID != idHome {
		t.Fatalf("first placement Home, got %s", onHome.DeviceID)
	}
	if onHome.Placement != "scheduled" {
		t.Fatalf("placement %+v", onHome)
	}

	stopHome()
	retried := waitJobStatus(t, cl, ctx, hang.ID, "running", "assigned")
	if retried.DeviceID != idVPS {
		t.Fatalf("retry elsewhere → VPS, got %+v", retried)
	}
	if retried.Attempts < 2 {
		t.Fatalf("expected retry attempt, got %+v", retried)
	}
	if _, err := cl.CancelJob(ctx, hang.ID); err != nil {
		t.Fatal(err)
	}
	done, err := cl.WaitJob(ctx, hang.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "canceled" {
		t.Fatalf("cancel after retry: %+v", done)
	}
}

func TestComputePinnedDisconnectStillFails(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	disc, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID:       idHome,
		Image:          "knot-fake-job:hang",
		TimeoutSeconds: 60,
		Resources:      client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	stopHome()
	time.Sleep(150 * time.Millisecond)
	dead, err := cl.GetJob(ctx, disc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != "failed" || !strings.Contains(dead.Error, "disconnected") {
		t.Fatalf("pinned must fail on disconnect: %+v", dead)
	}
}
