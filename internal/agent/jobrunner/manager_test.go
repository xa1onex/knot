package jobrunner

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestManagerRejectsOverPolicyBeforeRun(t *testing.T) {
	var got []protocol.JobResult
	var mu sync.Mutex
	m := NewManager(func(v any) error {
		if r, ok := v.(protocol.JobResult); ok {
			mu.Lock()
			got = append(got, r)
			mu.Unlock()
		}
		return nil
	}, t.TempDir())
	m.Policy = TestPolicy()
	m.Runner = NewFakeRunner(t.TempDir())

	raw, _ := json.Marshal(protocol.JobRun{
		Type: protocol.TypeJobRun, JobID: "big",
		Spec: protocol.JobSpec{JobID: "big", Image: "knot-fake-job:ok", Resources: protocol.JobResources{MemoryMB: 64 * 1024}},
	})
	m.Handle(raw)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Status != protocol.JobStatusRejected || got[0].Reason != protocol.JobReasonPolicyExceeded {
		t.Fatalf("%+v", got)
	}
}

func TestManagerConcurrencySlot(t *testing.T) {
	var got []protocol.JobResult
	var mu sync.Mutex
	dir := t.TempDir()
	m := NewManager(func(v any) error {
		if r, ok := v.(protocol.JobResult); ok {
			mu.Lock()
			got = append(got, r)
			mu.Unlock()
		}
		return nil
	}, dir)
	p := TestPolicy()
	p.MaxConcurrent = 1
	m.Policy = p
	m.Runner = NewFakeRunner(dir)

	hang, _ := json.Marshal(protocol.JobRun{
		Type: protocol.TypeJobRun, JobID: "h1", RequestID: "r1",
		Spec: protocol.JobSpec{JobID: "h1", Image: "knot-fake-job:hang", TimeoutSeconds: 5},
	})
	second, _ := json.Marshal(protocol.JobRun{
		Type: protocol.TypeJobRun, JobID: "h2", RequestID: "r2",
		Spec: protocol.JobSpec{JobID: "h2", Image: "knot-fake-job:ok", TimeoutSeconds: 5},
	})
	m.Handle(hang)
	time.Sleep(40 * time.Millisecond)
	m.Handle(second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, r := range got {
		if r.JobID == "h2" && r.Status == protocol.JobStatusRejected && r.Reason == protocol.JobReasonSlotUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected slot_unavailable, got %+v", got)
	}
	m.cancel("h1")
}
