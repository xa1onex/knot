package compute

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

func TestStatus(t *testing.T) {
	now := time.Now().UTC()
	if Status(false, &now, time.Minute) != protocol.ComputeStatusOffline {
		t.Fatal("offline device stays offline even with fresh snapshot")
	}
	if Status(true, nil, time.Minute) != protocol.ComputeStatusStale {
		t.Fatal("online without snapshot is stale")
	}
	old := now.Add(-2 * time.Hour)
	if Status(true, &old, time.Minute) != protocol.ComputeStatusStale {
		t.Fatal("old snapshot is stale")
	}
	if Status(true, &now, time.Minute) != protocol.ComputeStatusAvailable {
		t.Fatal("fresh online snapshot is available")
	}
}

func TestBuildPreservesNullGPU(t *testing.T) {
	inv := protocol.ComputeInventory{
		CPU:    protocol.ComputeCPU{Cores: 8, Architecture: "arm64"},
		Memory: protocol.ComputeMemory{TotalBytes: 16 << 30},
		GPU:    nil,
		Disks:  []protocol.ComputeDisk{{Mount: "/", TotalBytes: 1 << 40, FreeBytes: 1 << 39, UsedBytes: 1 << 39}},
	}
	b, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	d := store.Device{ID: "d1", Name: "Home PC", OS: "darwin", Arch: "arm64", Online: true, AgentVersion: "0.8.1"}
	rec := Build(d, &store.DeviceCompute{DeviceID: "d1", SnapshotJSON: string(b), CollectedAt: time.Now().UTC()}, time.Minute)
	if rec.Status != protocol.ComputeStatusAvailable {
		t.Fatalf("status %s", rec.Status)
	}
	if rec.GPU != nil {
		t.Fatalf("gpu must stay null, got %#v", rec.GPU)
	}
	if rec.CPU == nil || rec.CPU.Cores != 8 {
		t.Fatalf("cpu %+v", rec.CPU)
	}
}
