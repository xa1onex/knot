package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/agent/jobrunner"
	"github.com/knot-infra/knot/pkg/client"
	"github.com/knot-infra/knot/pkg/permissions"
)

func TestComputeJobHelloOnHomePC(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	job, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Command:   []string{"python", "/input/main.py"},
		Resources: client.JobResources{CPU: 2, MemoryMB: 512},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Resources.CPU != 2 || job.Resources.MemoryMB != 512 {
		t.Fatalf("resource limits not stored: %+v", job.Resources)
	}
	if job.Status != "queued" && job.Status != "running" && job.Status != "succeeded" && job.Status != "artifacts_committed" {
		t.Fatalf("unexpected start status %s", job.Status)
	}

	got, err := cl.WaitJob(ctx, job.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "artifacts_committed" {
		t.Fatalf("want artifacts_committed, got %+v", got)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("exit %+v", got.ExitCode)
	}

	logs, err := cl.JobLogs(ctx, job.ID, 50)
	if err != nil || len(logs) == 0 {
		t.Fatalf("logs: %v len=%d", err, len(logs))
	}
	hello := false
	for _, l := range logs {
		if strings.Contains(l.Message, "hello") {
			hello = true
		}
	}
	if !hello {
		t.Fatalf("expected hello in logs: %+v", logs)
	}

	ents, err := cl.StorageList(ctx, idHome, got.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ents {
		if e.Name == "result.txt" || strings.HasSuffix(e.Path, "result.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("artifact missing under %s: %+v", got.OutputPath, ents)
	}

	listed, err := cl.ListJobs(ctx, idHome)
	if err != nil || len(listed) == 0 {
		t.Fatalf("list: %v %+v", err, listed)
	}
}

func TestComputeJobFailureTimeoutCancelDisconnect(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	fail, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:fail",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	failGot, err := cl.WaitJob(ctx, fail.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if failGot.Status != "failed" {
		t.Fatalf("fail: %+v", failGot)
	}
	assertNoCommittedArtifacts(t, cl, ctx, idHome, fail.ID)

	to, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:sleep", TimeoutSeconds: 1,
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	toGot, err := cl.WaitJob(ctx, to.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if toGot.Status != "timeout" {
		t.Fatalf("timeout: %+v", toGot)
	}
	assertNoCommittedArtifacts(t, cl, ctx, idHome, to.ID)

	hang, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:hang", TimeoutSeconds: 30,
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := cl.CancelJob(ctx, hang.ID); err != nil {
		t.Fatal(err)
	}
	hangGot, err := cl.WaitJob(ctx, hang.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if hangGot.Status != "canceled" {
		t.Fatalf("cancel: %+v", hangGot)
	}
	assertNoCommittedArtifacts(t, cl, ctx, idHome, hang.ID)

	disc, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:hang", TimeoutSeconds: 60,
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	stopHome()
	time.Sleep(120 * time.Millisecond)
	dead, err := cl.GetJob(ctx, disc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != "failed" || !strings.Contains(dead.Error, "disconnected") {
		t.Fatalf("agent restart: %+v", dead)
	}
}

func TestComputeJobSecretRedactionAndScopes(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", t.TempDir())
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	job, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:secret",
		Env:       map[string]string{"JOB_SECRET": "s3cr3t-value", "PUBLIC": "ok"},
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.WaitJob(ctx, job.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["JOB_SECRET"] != "[redacted]" {
		t.Fatalf("env not redacted: %+v", got.Env)
	}
	if got.Env["PUBLIC"] != "ok" {
		t.Fatalf("public env: %+v", got.Env)
	}
	logs, err := cl.JobLogs(ctx, job.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l.Message, "s3cr3t-value") {
			t.Fatalf("secret leaked in logs: %q", l.Message)
		}
	}

	if _, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Command: []string{"bash", "-c", "rm -rf /"},
	}); err == nil || !client.IsValidation(err) {
		t.Fatalf("expected shell reject, got %v", err)
	}

	scheduled, err := cl.CreateJob(ctx, client.CreateJobRequest{Image: "python:3.13"})
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.Placement != "scheduled" {
		t.Fatalf("expected scheduled placement, got %+v", scheduled)
	}

	_, tok, err := cl.CreateCredential(ctx, "no-jobs", []string{permissions.ComputeRead, permissions.AccountAdmin}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(ts.URL, tok)
	if _, err := ro.ListJobs(ctx, ""); err != nil {
		t.Fatalf("compute.read should list: %v", err)
	}
	if _, err := ro.CreateJob(ctx, client.CreateJobRequest{DeviceID: idHome, Image: "python:3.13"}); err == nil || !client.IsForbidden(err) {
		t.Fatalf("expected forbidden without compute.write, got %v", err)
	}
}

func TestComputeJobInputArtifactViaStorage(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	payload := []byte("print('hello')\n")
	sum := sha256.Sum256(payload)
	if _, err := cl.StoragePut(ctx, idHome, "in/main.py", hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{}); err != nil {
		t.Fatal(err)
	}

	job, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Command:   []string{"python", "/input/main.py"},
		Resources: client.JobResources{CPU: 2, MemoryMB: 512},
		InputPath: "in",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.WaitJob(ctx, job.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "artifacts_committed" {
		t.Fatalf("%+v", got)
	}
	ents, err := cl.StorageList(ctx, idHome, got.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name] = true
	}
	if !names["result.txt"] || !names["main.py"] {
		t.Fatalf("expected input copy + result.txt in Storage: %+v", ents)
	}
}

func TestComputeJobResourceLimitsEnforcedByAgent(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	ram, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 64 * 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	ramGot, err := cl.WaitJob(ctx, ram.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if ramGot.Status != "rejected" || ramGot.Reason != "policy_exceeded" {
		t.Fatalf("64GB vs 8GB policy: %+v", ramGot)
	}

	gpu, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 512, GPU: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	gpuGot, err := cl.WaitJob(ctx, gpu.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if gpuGot.Status != "rejected" || gpuGot.Reason != "gpu_unavailable" {
		t.Fatalf("gpu unavailable: %+v", gpuGot)
	}

	cpu, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 16, MemoryMB: 512},
	})
	if err != nil {
		t.Fatal(err)
	}
	cpuGot, err := cl.WaitJob(ctx, cpu.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if cpuGot.Status != "rejected" || cpuGot.Reason != "policy_exceeded" {
		t.Fatalf("cpu policy: %+v", cpuGot)
	}

	pids, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 512, Pids: 512},
	})
	if err != nil {
		t.Fatal(err)
	}
	pidsGot, err := cl.WaitJob(ctx, pids.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if pidsGot.Status != "rejected" || pidsGot.Reason != "policy_exceeded" {
		t.Fatalf("pids policy: %+v", pidsGot)
	}

	oom, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:oom",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	oomGot, err := cl.WaitJob(ctx, oom.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if oomGot.Status != "failed" || oomGot.Reason != "resource_exceeded" {
		t.Fatalf("oom: %+v", oomGot)
	}

	disk, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:disk",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128, DiskMB: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	diskGot, err := cl.WaitJob(ctx, disk.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if diskGot.Status != "failed" || diskGot.Reason != "resource_exceeded" {
		t.Fatalf("disk: %+v", diskGot)
	}

	ok, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 2, MemoryMB: 512, Pids: 32, DiskMB: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	okGot, err := cl.WaitJob(ctx, ok.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if okGot.Status != "artifacts_committed" || okGot.Resources.Pids != 32 || okGot.Resources.DiskMB != 64 {
		t.Fatalf("within policy: %+v", okGot)
	}
}

func TestComputeJobGPULimitAndConcurrency(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	pol := jobrunner.TestPolicy()
	pol.MaxGPU = 1
	pol.GPURuntimeOK = true
	pol.MaxConcurrent = 1
	idHome, stopHome := registerAndConnectWithJobPolicy(t, ts, cl, "Home PC", share, pol)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	two, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128, GPU: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	twoGot, err := cl.WaitJob(ctx, two.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if twoGot.Status != "rejected" || twoGot.Reason != "gpu_unavailable" {
		t.Fatalf("gpu 2 vs 1: %+v", twoGot)
	}

	one, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128, GPU: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	oneGot, err := cl.WaitJob(ctx, one.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if oneGot.Status != "artifacts_committed" {
		t.Fatalf("gpu 1 allowed: %+v", oneGot)
	}

	hang, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:hang", TimeoutSeconds: 8,
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	slot, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	slotGot, err := cl.WaitJob(ctx, slot.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if slotGot.Status != "rejected" || slotGot.Reason != "slot_unavailable" {
		t.Fatalf("concurrency: %+v", slotGot)
	}
	_, _ = cl.CancelJob(ctx, hang.ID)
	_, _ = cl.WaitJob(ctx, hang.ID, 40*time.Millisecond)
}

func TestComputeJobArtifactsCommittedToStorage(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	payload := []byte("from-input\n")
	sum := sha256.Sum256(payload)
	if _, err := cl.StoragePut(ctx, idHome, "in/data.txt", hex.EncodeToString(sum[:]), int64(len(payload)), bytes.NewReader(payload), client.StoragePutOpts{}); err != nil {
		t.Fatal(err)
	}
	secret := []byte("should-not-leak")
	secSum := sha256.Sum256(secret)
	if _, err := cl.StoragePut(ctx, idHome, "secret.txt", hex.EncodeToString(secSum[:]), int64(len(secret)), bytes.NewReader(secret), client.StoragePutOpts{}); err != nil {
		t.Fatal(err)
	}

	job, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "python:3.13",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
		InputPath: "in",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.InputPath != "jobs/"+job.ID+"/input" || job.OutputPath != "jobs/"+job.ID+"/output" {
		t.Fatalf("canonical paths: %+v", job)
	}
	got, err := cl.WaitJob(ctx, job.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "artifacts_committed" {
		t.Fatalf("%+v", got)
	}

	hello := []byte("hello\n")
	helloSum := sha256.Sum256(hello)
	helloHex := hex.EncodeToString(helloSum[:])

	arts, err := cl.JobArtifacts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]client.JobArtifact{}
	for _, a := range arts {
		byName[a.Name] = a
		if a.JobID != job.ID || a.FileID == "" || a.SHA256 == "" || a.Path == "" {
			t.Fatalf("incomplete metadata: %+v", a)
		}
	}
	resArt, ok := byName["result.txt"]
	if !ok {
		t.Fatalf("result.txt missing: %+v", arts)
	}
	if resArt.SHA256 != helloHex {
		t.Fatalf("sha256 want %s got %s", helloHex, resArt.SHA256)
	}
	if _, ok := byName["hash.json"]; !ok {
		t.Fatalf("hash.json missing: %+v", arts)
	}
	if _, ok := byName["secret.txt"]; ok {
		t.Fatal("job saw storage-root secret.txt")
	}

	shown, err := cl.GetJob(ctx, job.ID)
	if err != nil || len(shown.Artifacts) == 0 {
		t.Fatalf("get job artifacts: %v %+v", err, shown)
	}

	st, err := cl.StorageStat(ctx, idHome, resArt.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.SHA256 != helloHex {
		t.Fatalf("storage sha256 %s want %s", st.SHA256, helloHex)
	}

	ents, err := cl.StorageList(ctx, idHome, got.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name] = true
	}
	if !names["result.txt"] || !names["hash.json"] || !names["data.txt"] {
		t.Fatalf("storage output: %+v", ents)
	}
	if names["secret.txt"] {
		t.Fatal("secret leaked into output")
	}
}

func TestComputeJobArtifactLimits(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	idHome, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	many, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:manyfiles",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128},
	})
	if err != nil {
		t.Fatal(err)
	}
	manyGot, err := cl.WaitJob(ctx, many.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if manyGot.Status != "failed" || manyGot.Reason != "artifact_limit" {
		t.Fatalf("many files: %+v", manyGot)
	}
	assertNoCommittedArtifacts(t, cl, ctx, idHome, many.ID)

	big, err := cl.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: idHome, Image: "knot-fake-job:bigfile",
		Resources: client.JobResources{CPU: 1, MemoryMB: 128, DiskMB: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	bigGot, err := cl.WaitJob(ctx, big.ID, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if bigGot.Status != "failed" || bigGot.Reason != "artifact_limit" {
		t.Fatalf("big file: %+v", bigGot)
	}
	assertNoCommittedArtifacts(t, cl, ctx, idHome, big.ID)
}

func TestComputeJobAgentRestartSweepsPartialOutput(t *testing.T) {
	ts, cl, _, _, _ := startCPFull(t, true, t.TempDir()+"/knot.db")
	share := t.TempDir()
	partial := filepath.Join(share, "storage", "jobs", "orphan", ".knot.part")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "x.bin"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stopHome := registerAndConnect(t, ts, cl, "Home PC", share)
	defer stopHome()
	time.Sleep(80 * time.Millisecond)
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("agent start must sweep leftover .knot.part")
	}
}

func assertNoCommittedArtifacts(t *testing.T, cl *client.Client, ctx context.Context, deviceID, jobID string) {
	t.Helper()
	arts, err := cl.JobArtifacts(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Fatalf("expected no artifacts for %s: %+v", jobID, arts)
	}
	ents, err := cl.StorageList(ctx, deviceID, "jobs/"+jobID+"/output")
	if err == nil {
		for _, e := range ents {
			if !e.IsDir && e.Name != "" {
				t.Fatalf("output present for incomplete job %s: %+v", jobID, ents)
			}
		}
	}
}
