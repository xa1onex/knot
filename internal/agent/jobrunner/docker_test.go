package jobrunner

import (
	"strings"
	"testing"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestPolicyRejectsMemoryAboveCeiling(t *testing.T) {
	p := TestPolicy()
	reason, msg := p.Check(protocol.JobSpec{Resources: protocol.JobResources{MemoryMB: 64 * 1024}})
	if reason != protocol.JobReasonPolicyExceeded || !strings.Contains(msg, "memory_mb") {
		t.Fatalf("%s %s", reason, msg)
	}
}

func TestPolicyRejectsCPUAboveCeiling(t *testing.T) {
	p := TestPolicy()
	reason, _ := p.Check(protocol.JobSpec{Resources: protocol.JobResources{CPU: 16}})
	if reason != protocol.JobReasonPolicyExceeded {
		t.Fatalf("%s", reason)
	}
}

func TestPolicyGPUUnavailableNoFallback(t *testing.T) {
	p := TestPolicy()
	reason, msg := p.Check(protocol.JobSpec{Resources: protocol.JobResources{GPU: 1}})
	if reason != protocol.JobReasonGPUUnavailable || !strings.Contains(msg, "gpu_unavailable") {
		t.Fatalf("%s %s", reason, msg)
	}
}

func TestPolicyGPUCountLimit(t *testing.T) {
	p := TestPolicy()
	p.MaxGPU = 1
	p.GPURuntimeOK = true
	reason, _ := p.Check(protocol.JobSpec{Resources: protocol.JobResources{GPU: 2}})
	if reason != protocol.JobReasonGPUUnavailable {
		t.Fatalf("%s", reason)
	}
	reason, _ = p.Check(protocol.JobSpec{Resources: protocol.JobResources{GPU: 1, MemoryMB: 512}})
	if reason != "" {
		t.Fatalf("gpu 1 should pass: %s", reason)
	}
}

func TestPolicyBoundNeverRaisesQuota(t *testing.T) {
	p := TestPolicy()
	got := p.Bound(protocol.JobResources{CPU: 64, MemoryMB: 65536, Pids: 99999, DiskMB: 99999})
	if got.CPU > p.MaxCPU || got.MemoryMB > p.MaxMemoryMB || got.Pids > p.MaxPids || got.DiskMB > p.MaxDiskMB {
		t.Fatalf("bound exceeded policy: %+v", got)
	}
}

func TestDockerJobArgsIsolation(t *testing.T) {
	p := TestPolicy()
	args := dockerJobArgs(protocol.JobSpec{
		Image:     "python:3.13",
		Command:   []string{"python", "/input/main.py"},
		Resources: protocol.JobResources{CPU: 2, MemoryMB: 512, GPU: 0, Pids: 128, DiskMB: 64},
	}, "knot-job-abc", "/tmp/in", "/tmp/out", p)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--network none",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--memory 512m",
		"--memory-swap 512m",
		"--cpus 2.00",
		"--pids-limit 128",
		"--tmpfs /tmp:rw,noexec,nosuid,size=64m",
		"-v /tmp/in:/input:ro",
		"-v /tmp/out:/output",
		"python:3.13",
		"python /input/main.py",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
	if strings.Contains(joined, "-d ") || strings.HasPrefix(joined, "run -d") {
		t.Fatal("jobs must run in foreground, not -d")
	}
	if strings.Contains(joined, "-p ") || strings.Contains(joined, "--publish") {
		t.Fatal("jobs must not publish ports")
	}
	if strings.Contains(joined, "cpu-shares") {
		t.Fatal("CPU must be a quota (--cpus), not shares")
	}
}

func TestDockerJobArgsCapsAtPolicy(t *testing.T) {
	p := TestPolicy()
	args := dockerJobArgs(protocol.JobSpec{
		Image:     "python:3.13",
		Resources: protocol.JobResources{CPU: 64, MemoryMB: 65536, Pids: 99999},
	}, "knot-job-x", "/i", "/o", p)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--cpus 64") || strings.Contains(joined, "--memory 65536m") {
		t.Fatalf("docker argv exceeded policy: %v", args)
	}
	if !strings.Contains(joined, "--cpus 4.00") || !strings.Contains(joined, "--memory 8192m") {
		t.Fatalf("expected capped argv, got %v", args)
	}
}

func TestDockerJobArgsGPUDevicesNotAll(t *testing.T) {
	p := TestPolicy()
	p.MaxGPU = 1
	p.GPURuntimeOK = true
	args := dockerJobArgs(protocol.JobSpec{
		Image:     "python:3.13",
		Resources: protocol.JobResources{GPU: 1},
	}, "knot-job-g", "/i", "/o", p)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--gpus device=0") {
		t.Fatalf("%v", args)
	}
	if strings.Contains(joined, "--gpus all") {
		t.Fatal("must not pass all GPUs")
	}
}

func TestDockerJobArgsNoGPUWhenRuntimeMissing(t *testing.T) {
	p := TestPolicy()
	args := dockerJobArgs(protocol.JobSpec{
		Image:     "python:3.13",
		Resources: protocol.JobResources{GPU: 1},
	}, "knot-job-g", "/i", "/o", p)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--gpus") {
		t.Fatalf("must not attach GPU without runtime: %v", args)
	}
}
