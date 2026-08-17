package deployrunner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

// FakeRunner runs knot-fake:* images as in-process HTTP servers (integration tests).
type FakeRunner struct {
	mu    sync.Mutex
	byKey map[string]*fakeWorkload
}

type fakeWorkload struct {
	id      string
	spec    protocol.DeploySpec
	server  *http.Server
	logs    []string
	healthy bool
	body    string
}

func NewFakeRunner() *FakeRunner {
	return &FakeRunner{byKey: make(map[string]*fakeWorkload)}
}

func (f *FakeRunner) Apply(ctx context.Context, depID, removeCID string, spec protocol.DeploySpec) (string, bool, []string, error) {
	if spec.Runtime != "docker" {
		return "", false, nil, fmt.Errorf("unsupported runtime %q", spec.Runtime)
	}
	if removeCID != "" {
		_, _, _ = f.removeKey(removeCID)
	}
	f.stopSameAddr(spec.Bind, spec.Port)

	healthy, body := fakeHealth(spec.Image)
	w := &fakeWorkload{
		id: depID, spec: spec, healthy: healthy, body: body,
		logs: []string{fmt.Sprintf("fake: starting %s on %s:%d", spec.Image, spec.Bind, spec.Port)},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(wr http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/env/") {
			key := strings.TrimPrefix(r.URL.Path, "/env/")
			_, _ = wr.Write([]byte(spec.Env[key]))
			return
		}
		if r.URL.Path == spec.HealthPath || r.URL.Path == "/" {
			if healthy {
				wr.WriteHeader(http.StatusOK)
				_, _ = wr.Write([]byte(body))
				return
			}
			http.Error(wr, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(wr, r)
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", spec.Bind, spec.Port))
	if err != nil {
		return "", false, w.logs, err
	}
	w.server = &http.Server{Handler: mux}
	go func() { _ = w.server.Serve(ln) }()

	ok, err := waitHealth(ctx, spec.Bind, spec.Port, spec.HealthPath, healthy)
	w.logs = append(w.logs, fmt.Sprintf("fake: health=%v", ok))

	f.mu.Lock()
	f.byKey[spec.Name] = w
	f.byKey[depID] = w
	f.mu.Unlock()

	if !ok {
		return depID, false, w.logs, nil
	}
	return depID, true, w.logs, err
}

func (f *FakeRunner) Stop(ctx context.Context, spec protocol.DeploySpec, containerID string) (string, []string, error) {
	w, logs, err := f.stopKey(containerID)
	if w == nil && containerID != spec.Name {
		w, logs, err = f.stopKey(spec.Name)
	}
	if w == nil {
		return "", logs, err
	}
	return w.id, logs, err
}

func (f *FakeRunner) Restart(ctx context.Context, depID string, spec protocol.DeploySpec, containerID string) (string, bool, []string, error) {
	f.mu.Lock()
	w := f.byKey[spec.Name]
	f.mu.Unlock()
	remove := ""
	if w != nil {
		remove = w.id
	}
	return f.Apply(ctx, depID, remove, spec)
}

func (f *FakeRunner) Remove(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	_, logs, err := f.removeKey(containerID)
	if err == nil {
		return logs, nil
	}
	_, logs2, err2 := f.removeKey(spec.Name)
	return append(logs, logs2...), err2
}

func (f *FakeRunner) Logs(ctx context.Context, spec protocol.DeploySpec, containerID string) ([]string, error) {
	f.mu.Lock()
	w := f.byKey[spec.Name]
	f.mu.Unlock()
	if w == nil {
		return []string{"no workload"}, nil
	}
	out := make([]string, len(w.logs))
	copy(out, w.logs)
	return out, nil
}

func (f *FakeRunner) stopByName(name string) *fakeWorkload {
	f.mu.Lock()
	w := f.byKey[name]
	f.mu.Unlock()
	if w == nil {
		return nil
	}
	_, _, _ = f.stopKey(name)
	return w
}

func (f *FakeRunner) stopKey(key string) (*fakeWorkload, []string, error) {
	f.mu.Lock()
	w := f.byKey[key]
	if w == nil {
		f.mu.Unlock()
		return nil, nil, nil
	}
	delete(f.byKey, key)
	if f.byKey[w.spec.Name] == w {
		delete(f.byKey, w.spec.Name)
	}
	if f.byKey[w.id] == w {
		delete(f.byKey, w.id)
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := w.server.Shutdown(ctx)
	w.logs = append(w.logs, "fake: stopped")
	return w, w.logs, err
}

func (f *FakeRunner) removeKey(key string) (*fakeWorkload, []string, error) {
	return f.stopKey(key)
}

func (f *FakeRunner) stopSameAddr(bind string, port int) {
	f.mu.Lock()
	var ids []string
	seen := map[*fakeWorkload]bool{}
	for _, w := range f.byKey {
		if w == nil || seen[w] {
			continue
		}
		seen[w] = true
		if w.spec.Bind == bind && w.spec.Port == port {
			ids = append(ids, w.id)
		}
	}
	f.mu.Unlock()
	for _, id := range ids {
		_, _, _ = f.stopKey(id)
	}
}

func fakeHealth(image string) (healthy bool, body string) {
	body = "knot-fake ok"
	healthy = true
	lower := strings.ToLower(image)
	if strings.Contains(lower, "unhealthy") || strings.Contains(lower, "v2-bad") {
		healthy = false
		body = "knot-fake unhealthy"
	}
	if strings.Contains(lower, ":v1") {
		body = "v1"
	}
	if strings.Contains(lower, ":v2") && healthy {
		body = "v2"
	}
	return healthy, body
}

func waitHealth(ctx context.Context, bind string, port int, path string, expectOK bool) (bool, error) {
	if path == "" {
		path = "/"
	}
	url := fmt.Sprintf("http://%s:%d%s", bind, port, path)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if expectOK && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return true, nil
			}
			if !expectOK && resp.StatusCode >= 500 {
				return false, nil
			}
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false, nil
}
