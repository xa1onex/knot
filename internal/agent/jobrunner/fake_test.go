package jobrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestFakeJobHello(t *testing.T) {
	dir := t.TempDir()
	f := NewFakeRunner(dir)
	res, err := f.Run(t.Context(), protocol.JobSpec{
		JobID: "j1", Image: "knot-fake-job:ok", OutputPath: "jobs/j1/output",
	}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusArtifactsCommitted || res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("%+v", res)
	}
	found := false
	for _, l := range res.LogLines {
		if l == "hello" {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs %v", res.LogLines)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "j1", "output", "result.txt")); err != nil {
		t.Fatal("artifact missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "j1", "output", "hash.json")); err != nil {
		t.Fatal("hash.json missing")
	}
	if len(res.Artifacts) < 2 {
		t.Fatalf("artifacts %+v", res.Artifacts)
	}
}

func TestFakeJobFail(t *testing.T) {
	dir := t.TempDir()
	f := NewFakeRunner(dir)
	res, err := f.Run(t.Context(), protocol.JobSpec{JobID: "jf", Image: "knot-fake-job:fail", OutputPath: "jobs/jf/output"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "jf", "output")); !os.IsNotExist(err) {
		t.Fatalf("failed job must not commit output: %v", err)
	}
}

func TestFakeJobTimeout(t *testing.T) {
	dir := t.TempDir()
	f := NewFakeRunner(dir)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	res, err := f.Run(ctx, protocol.JobSpec{JobID: "jt", Image: "knot-fake-job:sleep", OutputPath: "jobs/jt/output"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusTimeout {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "jt", "output")); !os.IsNotExist(err) {
		t.Fatalf("timeout must not commit: %v", err)
	}
}

func TestFakeJobCancel(t *testing.T) {
	dir := t.TempDir()
	f := NewFakeRunner(dir)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	res, err := f.Run(ctx, protocol.JobSpec{JobID: "jc", Image: "knot-fake-job:hang", OutputPath: "jobs/jc/output"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusCanceled {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "jc", "output")); !os.IsNotExist(err) {
		t.Fatalf("cancel must not commit: %v", err)
	}
}

func TestFakeJobOOM(t *testing.T) {
	f := NewFakeRunner(t.TempDir())
	res, err := f.Run(t.Context(), protocol.JobSpec{JobID: "jo", Image: "knot-fake-job:oom"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed || res.Reason != protocol.JobReasonResourceExceeded {
		t.Fatalf("%+v", res)
	}
}

func TestFakeJobDiskLimit(t *testing.T) {
	dir := t.TempDir()
	f := NewFakeRunner(dir)
	res, err := f.Run(t.Context(), protocol.JobSpec{
		JobID: "jd", Image: "knot-fake-job:disk", Resources: protocol.JobResources{DiskMB: 1}, OutputPath: "jobs/jd/output",
	}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed || res.Reason != protocol.JobReasonResourceExceeded {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "jd", "output")); !os.IsNotExist(err) {
		t.Fatalf("disk fail must not commit: %v", err)
	}
}

func TestFakeJobTooManyFiles(t *testing.T) {
	f := NewFakeRunner(t.TempDir())
	res, err := f.Run(t.Context(), protocol.JobSpec{JobID: "jm", Image: "knot-fake-job:manyfiles"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed || res.Reason != protocol.JobReasonArtifactLimit {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Error, "too many files") {
		t.Fatalf("error: %s", res.Error)
	}
}

func TestFakeJobSingleFileTooBig(t *testing.T) {
	f := NewFakeRunner(t.TempDir())
	res, err := f.Run(t.Context(), protocol.JobSpec{JobID: "jb", Image: "knot-fake-job:bigfile"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed || res.Reason != protocol.JobReasonArtifactLimit {
		t.Fatalf("%+v", res)
	}
}

func TestFakeJobInputIsolation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "in"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "in", "data.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFakeRunner(dir)
	res, err := f.Run(t.Context(), protocol.JobSpec{
		JobID: "ji", Image: "knot-fake-job:ok", SourcePath: "in", OutputPath: "jobs/ji/output",
	}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusArtifactsCommitted {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "ji", "output", "data.txt")); err != nil {
		t.Fatal("input data missing from output")
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", "ji", "output", "secret.txt")); !os.IsNotExist(err) {
		t.Fatal("job must not see storage-root files")
	}
}

func TestFakeJobTooDeep(t *testing.T) {
	f := NewFakeRunner(t.TempDir())
	res, err := f.Run(t.Context(), protocol.JobSpec{JobID: "jdn", Image: "knot-fake-job:deep"}, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != protocol.JobStatusFailed || res.Reason != protocol.JobReasonArtifactLimit {
		t.Fatalf("%+v", res)
	}
}

func TestSweepRemovesPartialOutput(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	partial := filepath.Join(root, "jobs", "dead", ".knot.part")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "x.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(work, "stale")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	sweepLeftovers(root, work)
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("partial output survived sweep")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("workspace leftover survived sweep")
	}
}
