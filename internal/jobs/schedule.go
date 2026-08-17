package jobs

import (
	"encoding/json"
	"strings"

	"github.com/knot-infra/knot/internal/compute"
	"github.com/knot-infra/knot/pkg/protocol"
)

const (
	decisionAssign        = "assign"
	decisionWait          = "wait"
	decisionUnsatisfiable = "unsatisfiable"
)

type scheduleReq struct {
	CPU      float64
	MemoryMB int64
	GPU      int
	DiskMB   int64
	Require  map[string]string
	Prefer   map[string]string
}

type scheduleNode struct {
	DeviceID    string
	Name        string
	Status      string
	HasSnapshot bool
	Online      bool
	CPUCores    float64
	MemoryMB    int64
	GPUCount    int
	NVIDIA      bool
	DiskFreeMB  int64
	DiskTotalMB int64
	Labels      map[string]string
	UsedCPU     float64
	UsedMemMB   int64
	UsedGPU     int
	UsedDiskMB  int64
}

func parseLabelMap(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]string{}
	}
	return out
}

func labelJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func mergeLabels(rec compute.Record, user map[string]string) map[string]string {
	return compute.LabelsFor(rec, user)
}

func gpuCount(rec compute.Record) int {
	if rec.GPU == nil {
		return 0
	}
	n := 0
	for _, g := range *rec.GPU {
		if g.Available != nil && !*g.Available {
			continue
		}
		n++
	}
	return n
}

func hasNVIDIA(rec compute.Record) bool {
	if rec.GPU == nil {
		return false
	}
	for _, g := range *rec.GPU {
		if strings.Contains(strings.ToLower(g.Vendor+" "+g.Model), "nvidia") ||
			strings.Contains(strings.ToLower(g.Model), "rtx") ||
			strings.Contains(strings.ToLower(g.Model), "gtx") {
			return true
		}
	}
	return false
}

func nodeFromRecord(rec compute.Record, userLabels map[string]string) scheduleNode {
	n := scheduleNode{
		DeviceID: rec.DeviceID,
		Name:     rec.Name,
		Status:   rec.Status,
		Online:   rec.Online,
		Labels:   mergeLabels(rec, userLabels),
	}
	if rec.CPU != nil {
		n.CPUCores = float64(rec.CPU.Cores)
		n.HasSnapshot = true
	}
	if rec.Memory != nil {
		n.MemoryMB = int64(rec.Memory.TotalBytes / (1024 * 1024))
		n.HasSnapshot = true
	}
	if rec.GPU != nil {
		n.HasSnapshot = true
		n.GPUCount = gpuCount(rec)
		n.NVIDIA = hasNVIDIA(rec)
	}
	for _, d := range rec.Disks {
		n.HasSnapshot = true
		free := int64(d.FreeBytes / (1024 * 1024))
		total := int64(d.TotalBytes / (1024 * 1024))
		if free > n.DiskFreeMB {
			n.DiskFreeMB = free
		}
		if total > n.DiskTotalMB {
			n.DiskTotalMB = total
		}
	}
	return n
}

func labelsMatch(have map[string]string, need map[string]string) bool {
	for k, v := range need {
		got, ok := have[strings.ToLower(k)]
		if !ok {
			got, ok = have[k]
		}
		if !ok || got != v {
			return false
		}
	}
	return true
}

func capable(req scheduleReq, n scheduleNode) bool {
	if !n.HasSnapshot {
		return false
	}
	if req.CPU > 0 && n.CPUCores+1e-9 < req.CPU {
		return false
	}
	if req.MemoryMB > 0 && n.MemoryMB < req.MemoryMB {
		return false
	}
	if req.GPU > 0 && n.GPUCount < req.GPU {
		return false
	}
	if req.DiskMB > 0 && n.DiskTotalMB > 0 && n.DiskTotalMB < req.DiskMB {
		return false
	}
	return labelsMatch(n.Labels, req.Require)
}

func allocatable(req scheduleReq, n scheduleNode) bool {
	if n.Status != protocol.ComputeStatusAvailable || !n.Online {
		return false
	}
	if !capable(req, n) {
		return false
	}
	if req.CPU > 0 && (n.CPUCores-n.UsedCPU)+1e-9 < req.CPU {
		return false
	}
	if req.MemoryMB > 0 && n.MemoryMB-n.UsedMemMB < req.MemoryMB {
		return false
	}
	if req.GPU > 0 && n.GPUCount-n.UsedGPU < req.GPU {
		return false
	}
	if req.DiskMB > 0 && n.DiskFreeMB > 0 && n.DiskFreeMB-n.UsedDiskMB < req.DiskMB {
		return false
	}
	return true
}

func score(req scheduleReq, n scheduleNode) int {
	s := 0
	for k, v := range req.Prefer {
		got, ok := n.Labels[strings.ToLower(k)]
		if !ok {
			got = n.Labels[k]
		}
		if got == v {
			s += 1000
		}
	}
	if req.GPU > 0 && n.NVIDIA {
		s += 100
	}
	s += int(n.CPUCores - n.UsedCPU)
	if n.MemoryMB > n.UsedMemMB {
		s += int((n.MemoryMB - n.UsedMemMB) / 1024)
	}
	return s
}

func pickNode(req scheduleReq, nodes []scheduleNode) (deviceID, decision string) {
	var best *scheduleNode
	bestScore := -1 << 30
	anyCapable := false
	anyUnknownOnline := false
	for i := range nodes {
		n := &nodes[i]
		if n.Online && !n.HasSnapshot {
			anyUnknownOnline = true
		}
		if capable(req, *n) {
			anyCapable = true
		}
		if !allocatable(req, *n) {
			continue
		}
		sc := score(req, *n)
		if best == nil || sc > bestScore || (sc == bestScore && n.DeviceID < best.DeviceID) {
			cp := *n
			best = &cp
			bestScore = sc
		}
	}
	if best != nil {
		return best.DeviceID, decisionAssign
	}
	if anyCapable || anyUnknownOnline {
		return "", decisionWait
	}
	return "", decisionUnsatisfiable
}
