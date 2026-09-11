// Package ratelimit provides a small, dependency-free per-key rate limiter
// used to slow down abuse of sensitive endpoints (login, sharing) without
// requiring an external store.
package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter: each key gets at most Max events
// per Window, after which further events are rejected until the window
// resets. Safe for concurrent use.
type Limiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count      int
	windowEnds time.Time
}

// NewLimiter constructs a Limiter allowing at most max events per window,
// per key.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, buckets: make(map[string]*bucket)}
}

// Allow reports whether an event for key is permitted right now, recording
// it if so. It also opportunistically evicts expired buckets so memory
// doesn't grow unbounded over time.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) > 10000 {
		l.evictExpiredLocked(now)
	}

	b, ok := l.buckets[key]
	if !ok || now.After(b.windowEnds) {
		l.buckets[key] = &bucket{count: 1, windowEnds: now.Add(l.window)}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

func (l *Limiter) evictExpiredLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.After(b.windowEnds) {
			delete(l.buckets, k)
		}
	}
}

// ClientIP extracts the caller's address for rate-limiting purposes:
// X-Real-IP (set by the nginx reverse proxy in front of this app, from its
// own view of the TCP peer, so it can't be spoofed by the client) if
// present, else the request's own remote address.
func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
