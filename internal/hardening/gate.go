package hardening

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/knot-infra/knot/internal/ratelimit"
	"github.com/knot-infra/knot/pkg/apierrors"
)

const (
	CodeRateLimited = "rate_limited"
	CodeLocked      = "account_locked"
)

type Metrics struct {
	startedAt time.Time
	reqTotal  atomic.Int64
	reqDenied atomic.Int64
}

func NewMetrics() *Metrics {
	return &Metrics{startedAt: time.Now().UTC()}
}

func (m *Metrics) IncReq()    { m.reqTotal.Add(1) }
func (m *Metrics) IncDenied() { m.reqDenied.Add(1) }
func (m *Metrics) Snapshot() map[string]any {
	return map[string]any{
		"uptime_sec":  int64(time.Since(m.startedAt).Seconds()),
		"http_total":  m.reqTotal.Load(),
		"http_denied": m.reqDenied.Load(),
	}
}

type Gate struct {
	Login      *ratelimit.Limiter
	API        *ratelimit.Limiter
	TrustProxy bool
}

func NewGate(loginPerMin, apiPerMin int) *Gate {
	return &Gate{
		Login: ratelimit.New(loginPerMin, time.Minute),
		API:   ratelimit.New(apiPerMin, time.Minute),
	}
}

func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func WriteLimited(w http.ResponseWriter) {
	apierrors.WriteCode(w, http.StatusTooManyRequests, CodeRateLimited, "rate limited")
}

func Middleware(g *Gate, m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m != nil {
				m.IncReq()
			}
			path := r.URL.Path
			probe := path == "/healthz" || path == "/readyz" || path == "/metrics"
			trust := g != nil && g.TrustProxy
			if !probe && g != nil && g.API != nil {
				key := ClientIP(r, trust)
				if path == "/v1/auth/login" {
					if !g.Login.Allow(key) {
						if m != nil {
							m.IncDenied()
						}
						WriteLimited(w)
						logAccess(r, trust, http.StatusTooManyRequests, 0)
						return
					}
				} else if strings.HasPrefix(path, "/v1/") {
					if !g.API.Allow(key) {
						if m != nil {
							m.IncDenied()
						}
						WriteLimited(w)
						logAccess(r, trust, http.StatusTooManyRequests, 0)
						return
					}
				}
			}
			if probe {
				next.ServeHTTP(w, r)
				return
			}
			sw := newStatusWriter(w)
			start := time.Now()
			next.ServeHTTP(sw, r)
			logAccess(r, trust, sw.status, time.Since(start))
		})
	}
}

func logAccess(r *http.Request, trustProxy bool, status int, d time.Duration) {
	log.Printf("http %s %s %s %d %s", ClientIP(r, trustProxy), r.Method, r.URL.Path, status, d.Truncate(time.Millisecond))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	return h.Hijack()
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
