package buildrunner

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestFakePrivateRequiresToken(t *testing.T) {
	f := NewFakeRunner()
	var logs []string
	res := f.Run(context.Background(), protocol.BuildSpec{URL: "knot-fake-git:private", Tag: "app:v1"}, func(_, line string) {
		logs = append(logs, line)
	}, func(string) {})
	if res.Status != protocol.BuildStatusFailedClone {
		t.Fatalf("status=%s", res.Status)
	}
	res = f.Run(context.Background(), protocol.BuildSpec{URL: "knot-fake-git:private", Tag: "app:v1", GitToken: "s3cr3t"}, func(_, line string) {
		logs = append(logs, line)
	}, func(string) {})
	if !res.OK || res.Status != protocol.BuildStatusCompleted {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
}

func TestFakeBadDockerfile(t *testing.T) {
	f := NewFakeRunner()
	res := f.Run(context.Background(), protocol.BuildSpec{URL: "knot-fake-git:bad-dockerfile", Tag: "app:v1"}, func(string, string) {}, func(string) {})
	if res.Status != protocol.BuildStatusFailedBuild {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestManagerRedactsToken(t *testing.T) {
	var mu sync.Mutex
	var frames []any
	m := NewManager(func(v any) error {
		mu.Lock()
		frames = append(frames, v)
		mu.Unlock()
		return nil
	}, t.TempDir())
	m.Runner = NewFakeRunner()
	raw, _ := json.Marshal(protocol.BuildRun{
		Type: protocol.TypeBuildRun, BuildID: "b1", RequestID: "r1",
		Spec: protocol.BuildSpec{URL: "knot-fake-git:ok", Tag: "app:v1", GitToken: "UNIQUE-TOKEN-xyz", TimeoutSeconds: 10},
	})
	m.Handle(raw)
	deadline := time.Now().Add(2 * time.Second)
	var blob []byte
	for time.Now().Before(deadline) {
		mu.Lock()
		copyFrames := append([]any(nil), frames...)
		mu.Unlock()
		done := false
		for _, f := range copyFrames {
			if res, ok := f.(protocol.BuildResult); ok && protocol.BuildTerminal(res.Status) {
				done = true
				break
			}
		}
		if done {
			blob, _ = json.Marshal(copyFrames)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(blob) == 0 {
		t.Fatal("no build result")
	}
	if strings.Contains(string(blob), "UNIQUE-TOKEN-xyz") {
		t.Fatalf("token leaked: %s", blob)
	}
}
