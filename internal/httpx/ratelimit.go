// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Per-client token-bucket rate limiter. The limiter is dependency-free and
// in-process, which is sufficient for the thin NS layer; multi-replica
// deployments that need a shared budget can put a gateway/ingress limiter in
// front. Clients are keyed by the immediate peer address unless an
// X-Forwarded-For chain was injected by the trusted ingress.

package httpx

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// tokenBucket is a lazy-refill token bucket for one client.
type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter holds one bucket per client key.
type RateLimiter struct {
	mu              sync.Mutex
	buckets         map[string]*tokenBucket
	tokensPerMinute float64
	burst           float64
}

// NewRateLimiter builds a limiter allowing requestsPerMinute steady with the
// given short-term burst capacity per client.
func NewRateLimiter(requestsPerMinute, burst int) *RateLimiter {
	return &RateLimiter{
		buckets:         make(map[string]*tokenBucket),
		tokensPerMinute: float64(requestsPerMinute),
		burst:           float64(burst),
	}
}

// allow reports whether a request from key is admitted right now.
func (l *RateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = b
	}

	elapsedMinutes := now.Sub(b.lastSeen).Minutes()
	b.lastSeen = now
	b.tokens += elapsedMinutes * l.tokensPerMinute
	if b.tokens > l.burst {
		b.tokens = l.burst
	}

	// Opportunistically bound memory by dropping idle buckets periodically.
	if len(l.buckets) > 10_000 {
		for k, v := range l.buckets {
			if now.Sub(v.lastSeen) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// clientIP extracts the caller key, honouring the first X-Forwarded-For entry
// when present (NS is expected to run behind a trusted ingress).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return canonicalHost(xff[:i])
			}
		}
		return canonicalHost(xff)
	}
	return canonicalHost(r.RemoteAddr)
}

// canonicalHost strips the port from a host:port style address.
func canonicalHost(raw string) string {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return raw
}

// RateLimitMiddleware rejects over-budget clients with a 429 envelope and a
// Retry-After hint. The hint is a coarse 60s estimate.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.allow(clientIP(r), time.Now()) {
				w.Header().Set("Retry-After", strconv.Itoa(60))
				WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
