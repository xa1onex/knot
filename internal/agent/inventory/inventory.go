package inventory

import (
	"runtime"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

// Collect returns a hardware snapshot. GPU is nil when it cannot be detected.
func Collect() protocol.ComputeInventory {
	cpuUsage := cpuUsagePercent()
	mem := readMemory()
	inv := protocol.ComputeInventory{
		CPU: protocol.ComputeCPU{
			Cores:        runtime.NumCPU(),
			Architecture: runtime.GOARCH,
			UsagePercent: cpuUsage,
		},
		Memory: mem,
		GPU:    readGPUs(),
		Disks:  readDisks(),
	}
	if inv.Disks == nil {
		inv.Disks = []protocol.ComputeDisk{}
	}
	return inv
}

func pct(used, total uint64) *float64 {
	if total == 0 {
		return nil
	}
	v := float64(used) * 100 / float64(total)
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return &v
}

func sampleCPU(read func() (idle, total uint64, ok bool)) *float64 {
	i1, t1, ok := read()
	if !ok || t1 == 0 {
		return nil
	}
	time.Sleep(150 * time.Millisecond)
	i2, t2, ok := read()
	if !ok || t2 <= t1 {
		return nil
	}
	idle := i2 - i1
	total := t2 - t1
	if total == 0 {
		return nil
	}
	used := 100 - (float64(idle) * 100 / float64(total))
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return &used
}

func boolPtr(v bool) *bool { return &v }

func uintPtr(v uint64) *uint64 { return &v }
