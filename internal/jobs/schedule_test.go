package jobs

import (
	"testing"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestPickNodeGPUGoesToHomePC(t *testing.T) {
	req := scheduleReq{CPU: 8, MemoryMB: 16384, GPU: 1, DiskMB: 20480}
	home := scheduleNode{
		DeviceID: "home", Name: "Home PC", Status: protocol.ComputeStatusAvailable,
		HasSnapshot: true, Online: true, CPUCores: 16, MemoryMB: 65536, GPUCount: 1, NVIDIA: true,
		DiskFreeMB: 100000, DiskTotalMB: 2000000, Labels: map[string]string{"gpu": "true", "location": "home"},
	}
	vps := scheduleNode{
		DeviceID: "vps", Name: "VPS", Status: protocol.ComputeStatusAvailable,
		HasSnapshot: true, Online: true, CPUCores: 4, MemoryMB: 8192, GPUCount: 0,
		DiskFreeMB: 20000, DiskTotalMB: 80000, Labels: map[string]string{"public": "true", "linux": "true"},
	}
	id, dec := pickNode(req, []scheduleNode{vps, home})
	if dec != decisionAssign || id != "home" {
		t.Fatalf("got %s %s", id, dec)
	}
}

func TestPickNodeUnsatisfiable(t *testing.T) {
	req := scheduleReq{CPU: 8, MemoryMB: 16384, GPU: 1}
	vps := scheduleNode{
		DeviceID: "vps", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 4, MemoryMB: 8192,
	}
	_, dec := pickNode(req, []scheduleNode{vps})
	if dec != decisionUnsatisfiable {
		t.Fatalf("decision %s", dec)
	}
}

func TestPickNodeWaitsWhenBusy(t *testing.T) {
	req := scheduleReq{CPU: 8, GPU: 1}
	home := scheduleNode{
		DeviceID: "home", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 16, MemoryMB: 65536, GPUCount: 1, NVIDIA: true, UsedGPU: 1, UsedCPU: 8,
	}
	id, dec := pickNode(req, []scheduleNode{home})
	if dec != decisionWait || id != "" {
		t.Fatalf("got %s %s", id, dec)
	}
}

func TestPickNodePreferLabels(t *testing.T) {
	req := scheduleReq{CPU: 1, Prefer: map[string]string{"location": "home"}}
	a := scheduleNode{
		DeviceID: "a", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 8, MemoryMB: 8192, Labels: map[string]string{"location": "cloud"},
	}
	b := scheduleNode{
		DeviceID: "b", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 8, MemoryMB: 8192, Labels: map[string]string{"location": "home"},
	}
	id, dec := pickNode(req, []scheduleNode{a, b})
	if dec != decisionAssign || id != "b" {
		t.Fatalf("got %s %s", id, dec)
	}
}

func TestPickNodeWaitsUnknownOnline(t *testing.T) {
	req := scheduleReq{CPU: 8, GPU: 1}
	unknown := scheduleNode{DeviceID: "new", Status: protocol.ComputeStatusStale, Online: true, HasSnapshot: false}
	id, dec := pickNode(req, []scheduleNode{unknown})
	if dec != decisionWait || id != "" {
		t.Fatalf("got %s %s", id, dec)
	}
}

func TestPickNodeOfflineCapableWaits(t *testing.T) {
	req := scheduleReq{CPU: 8, GPU: 1}
	home := scheduleNode{
		DeviceID: "home", Status: protocol.ComputeStatusOffline, HasSnapshot: true, Online: false,
		CPUCores: 16, MemoryMB: 65536, GPUCount: 1, NVIDIA: true,
	}
	id, dec := pickNode(req, []scheduleNode{home})
	if dec != decisionWait || id != "" {
		t.Fatalf("got %s %s", id, dec)
	}
}

func TestPickNodeRequireLabels(t *testing.T) {
	req := scheduleReq{CPU: 1, Require: map[string]string{"gpu": "true"}}
	cpuOnly := scheduleNode{
		DeviceID: "cpu", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 16, MemoryMB: 65536, Labels: map[string]string{"linux": "true"},
	}
	gpuNode := scheduleNode{
		DeviceID: "gpu", Status: protocol.ComputeStatusAvailable, HasSnapshot: true, Online: true,
		CPUCores: 16, MemoryMB: 65536, GPUCount: 1, Labels: map[string]string{"gpu": "true"},
	}
	id, dec := pickNode(req, []scheduleNode{cpuOnly, gpuNode})
	if dec != decisionAssign || id != "gpu" {
		t.Fatalf("got %s %s", id, dec)
	}
}
