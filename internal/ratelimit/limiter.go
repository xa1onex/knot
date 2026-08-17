package ratelimit

import (
	"sync"
	"time"
)

type window struct {
	n     int
	reset time.Time
}

// Limiter is a simple per-key sliding window (in-memory; per-process).
type Limiter struct {
	mu      sync.Mutex
	windows map[string]window
	limit   int
	every   time.Duration
}

func New(limit int, every time.Duration) *Limiter {
	if limit <= 0 {
		limit = 60
	}
	if every <= 0 {
		every = time.Minute
	}
	return &Limiter{windows: make(map[string]window), limit: limit, every: every}
}

func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[key]
	if !ok || now.After(w.reset) {
		l.windows[key] = window{n: 1, reset: now.Add(l.every)}
		return true
	}
	if w.n >= l.limit {
		return false
	}
	w.n++
	l.windows[key] = w
	return true
}
