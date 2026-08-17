package jobrunner

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/knot-infra/knot/pkg/protocol"
)

// Policy is the agent's local job ceiling (Stage 8.3). It is the last
// enforcement point: a request above the ceiling is rejected, never applied
// as a larger Docker quota, and GPU requests never silently fall back to CPU.
type Policy struct {
	MaxCPU           float64
	MaxMemoryMB      int64
	MaxGPU           int
	GPURuntimeOK     bool
	MaxDiskMB        int64
	MaxPids          int64
	MaxTimeoutSec    int
	MaxConcurrent    int
	MaxArtifactBytes int64
	MaxArtifactFiles int
	MaxArtifactDirs  int
	MaxArtifactDepth int
}

// LoadPolicy reads host/env ceilings. Defaults: CPU=NumCPU, RAM=8 GiB,
// disk=4 GiB, pids=256, timeout=3600s, concurrent=4, GPU only if the NVIDIA
// container runtime is actually available.
func LoadPolicy() Policy {
	p := Policy{
		MaxCPU:           float64(runtime.NumCPU()),
		MaxMemoryMB:      protocol.DefaultJobPolicyMemMB,
		MaxDiskMB:        4 * 1024,
		MaxPids:          protocol.DefaultJobPids,
		MaxTimeoutSec:    protocol.MaxJobTimeout,
		MaxConcurrent:    protocol.DefaultJobConcurrent,
		MaxArtifactBytes: protocol.DefaultMaxArtifactBytes,
		MaxArtifactFiles: protocol.DefaultMaxArtifactFiles,
		MaxArtifactDirs:  protocol.DefaultMaxArtifactDirs,
		MaxArtifactDepth: protocol.DefaultMaxArtifactDepth,
	}
	n, ok := detectGPURuntime()
	p.MaxGPU = n
	p.GPURuntimeOK = ok
	p.applyEnv()
	return p.normalized()
}

// TestPolicy is the 8.3 criterion ceiling used by integration tests:
// 4 CPU, 8 GiB RAM, no GPU, 1 GiB disk, 4 slots.
func TestPolicy() Policy {
	return Policy{
		MaxCPU:           4,
		MaxMemoryMB:      protocol.DefaultJobPolicyMemMB,
		MaxGPU:           0,
		GPURuntimeOK:     false,
		MaxDiskMB:        protocol.DefaultJobDiskMB,
		MaxPids:          protocol.DefaultJobPids,
		MaxTimeoutSec:    protocol.MaxJobTimeout,
		MaxConcurrent:    protocol.DefaultJobConcurrent,
		MaxArtifactBytes: 1 << 20,
		MaxArtifactFiles: protocol.DefaultMaxArtifactFiles,
		MaxArtifactDirs:  protocol.DefaultMaxArtifactDirs,
		MaxArtifactDepth: protocol.DefaultMaxArtifactDepth,
	}.normalized()
}

func (p *Policy) applyEnv() {
	if v := os.Getenv("KNOT_JOB_MAX_CPU"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			p.MaxCPU = f
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_MEMORY_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			p.MaxMemoryMB = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_GPU"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.MaxGPU = n
			p.GPURuntimeOK = n > 0
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_DISK_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			p.MaxDiskMB = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_PIDS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			p.MaxPids = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.MaxTimeoutSec = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.MaxConcurrent = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_ARTIFACT_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			p.MaxArtifactBytes = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_ARTIFACT_FILES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.MaxArtifactFiles = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_ARTIFACT_DIRS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.MaxArtifactDirs = n
		}
	}
	if v := os.Getenv("KNOT_JOB_MAX_ARTIFACT_DEPTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.MaxArtifactDepth = n
		}
	}
}

func (p Policy) normalized() Policy {
	if p.MaxCPU <= 0 {
		p.MaxCPU = float64(runtime.NumCPU())
		if p.MaxCPU <= 0 {
			p.MaxCPU = protocol.DefaultJobCPUs
		}
	}
	if p.MaxMemoryMB <= 0 {
		p.MaxMemoryMB = protocol.DefaultJobPolicyMemMB
	}
	if p.MaxDiskMB <= 0 {
		p.MaxDiskMB = protocol.DefaultJobDiskMB
	}
	if p.MaxPids <= 0 {
		p.MaxPids = protocol.DefaultJobPids
	}
	if p.MaxPids > protocol.MaxJobPids {
		p.MaxPids = protocol.MaxJobPids
	}
	if p.MaxTimeoutSec <= 0 {
		p.MaxTimeoutSec = protocol.MaxJobTimeout
	}
	if p.MaxConcurrent <= 0 {
		p.MaxConcurrent = protocol.DefaultJobConcurrent
	}
	if p.MaxGPU < 0 {
		p.MaxGPU = 0
	}
	if p.MaxGPU == 0 {
		p.GPURuntimeOK = false
	}
	if p.MaxArtifactBytes <= 0 {
		p.MaxArtifactBytes = protocol.DefaultMaxArtifactBytes
	}
	if p.MaxArtifactFiles <= 0 {
		p.MaxArtifactFiles = protocol.DefaultMaxArtifactFiles
	}
	if p.MaxArtifactDirs <= 0 {
		p.MaxArtifactDirs = protocol.DefaultMaxArtifactDirs
	}
	if p.MaxArtifactDepth <= 0 {
		p.MaxArtifactDepth = protocol.DefaultMaxArtifactDepth
	}
	return p
}

// Check rejects a spec that exceeds the local ceiling. It never clamps.
func (p Policy) Check(spec protocol.JobSpec) (reason, message string) {
	p = p.normalized()
	res := spec.Resources
	cpu := res.CPU
	if cpu <= 0 {
		cpu = protocol.DefaultJobCPUs
	}
	mem := res.MemoryMB
	if mem <= 0 {
		mem = protocol.DefaultJobMemoryMB
	}
	pids := res.Pids
	if pids <= 0 {
		pids = protocol.DefaultJobPids
	}
	disk := res.DiskMB
	if disk <= 0 {
		disk = protocol.DefaultJobDiskMB
	}
	timeout := spec.TimeoutSeconds
	if timeout <= 0 {
		timeout = protocol.DefaultJobTimeout
	}

	if cpu > p.MaxCPU {
		return protocol.JobReasonPolicyExceeded, fmt.Sprintf("cpu %.2f exceeds node policy %.2f", cpu, p.MaxCPU)
	}
	if mem > p.MaxMemoryMB {
		return protocol.JobReasonPolicyExceeded, fmt.Sprintf("memory_mb %d exceeds node policy %d", mem, p.MaxMemoryMB)
	}
	if pids < protocol.MinJobPids || pids > p.MaxPids {
		return protocol.JobReasonPolicyExceeded, fmt.Sprintf("pids %d exceeds node policy %d", pids, p.MaxPids)
	}
	if disk > p.MaxDiskMB {
		return protocol.JobReasonPolicyExceeded, fmt.Sprintf("disk_mb %d exceeds node policy %d", disk, p.MaxDiskMB)
	}
	if timeout > p.MaxTimeoutSec {
		return protocol.JobReasonPolicyExceeded, fmt.Sprintf("timeout %ds exceeds node policy %ds", timeout, p.MaxTimeoutSec)
	}
	if res.GPU > 0 {
		if !p.GPURuntimeOK || p.MaxGPU <= 0 {
			return protocol.JobReasonGPUUnavailable, "gpu_unavailable: no GPU runtime on this node"
		}
		if res.GPU > p.MaxGPU {
			return protocol.JobReasonGPUUnavailable, fmt.Sprintf("gpu_unavailable: requested %d, policy allows %d", res.GPU, p.MaxGPU)
		}
	}
	return "", ""
}

// Bound fills defaults then caps each resource at the policy ceiling.
// Used only when constructing Docker argv so a bypassed Check cannot raise quotas.
// GPU is never silently zeroed (that would be a CPU fallback).
func (p Policy) Bound(res protocol.JobResources) protocol.JobResources {
	p = p.normalized()
	out := res
	if out.CPU <= 0 {
		out.CPU = protocol.DefaultJobCPUs
	}
	if p.MaxCPU > 0 && out.CPU > p.MaxCPU {
		out.CPU = p.MaxCPU
	}
	if out.MemoryMB <= 0 {
		out.MemoryMB = protocol.DefaultJobMemoryMB
	}
	if p.MaxMemoryMB > 0 && out.MemoryMB > p.MaxMemoryMB {
		out.MemoryMB = p.MaxMemoryMB
	}
	if out.Pids <= 0 {
		out.Pids = protocol.DefaultJobPids
	}
	if out.Pids < protocol.MinJobPids {
		out.Pids = protocol.MinJobPids
	}
	if p.MaxPids > 0 && out.Pids > p.MaxPids {
		out.Pids = p.MaxPids
	}
	if out.DiskMB <= 0 {
		out.DiskMB = protocol.DefaultJobDiskMB
	}
	if p.MaxDiskMB > 0 && out.DiskMB > p.MaxDiskMB {
		out.DiskMB = p.MaxDiskMB
	}
	return out
}

func detectGPURuntime() (int, bool) {
	if !nvidiaRuntimePresent() {
		return 0, false
	}
	out, err := exec.Command("nvidia-smi", "-L").Output()
	if err != nil {
		return 0, false
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "GPU ") {
			n++
		}
	}
	if n == 0 {
		n = 1
	}
	return n, true
}

func nvidiaRuntimePresent() bool {
	if err := exec.Command("nvidia-smi", "-L").Run(); err == nil {
		return true
	}
	out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "nvidia")
}

func rejectedResult(spec protocol.JobSpec, reason, message string) protocol.JobResult {
	return protocol.JobResult{
		JobID:  spec.JobID,
		OK:     false,
		Status: protocol.JobStatusRejected,
		Reason: reason,
		Error:  message,
	}
}
