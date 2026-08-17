package webui

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Wrap serves the Web panel for non-API paths from dir (SPA fallback to index.html).
// API probes stay on the control plane: /v1, /healthz, /readyz, /metrics.
func Wrap(api http.Handler, dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	root := filepath.Clean(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			api.ServeHTTP(w, r)
			return
		}
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		full := filepath.Join(root, filepath.FromSlash(rel))
		if !withinRoot(root, full) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func isAPIPath(p string) bool {
	return strings.HasPrefix(p, "/v1/") || p == "/v1" || p == "/healthz" || p == "/readyz" || p == "/metrics"
}

func withinRoot(root, full string) bool {
	root = filepath.Clean(root)
	full = filepath.Clean(full)
	sep := string(os.PathSeparator)
	return full == root || strings.HasPrefix(full, root+sep)
}
