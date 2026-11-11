// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package httpx holds the small set of HTTP transport primitives shared by
// the NereidSphere handlers: the family-wide JSON error envelope, request
// tracing and response helpers. Keeping these primitives in one owner
// prevents each handler from inventing its own response or trace shape.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// traceIDKey is the context key carrying the request correlation ID.
type traceIDKey struct{}

// traceHeader is the inbound/outbound correlation header, shared with the
// rest of the suite so a request can be followed across products.
const traceHeader = "X-Request-ID"

// TraceID returns the correlation ID stored on ctx, or "" when absent.
func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey{}).(string)
	return v
}

// WithTraceID returns ctx carrying the given correlation ID.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, id)
}

// traceWriter records the status code and bytes written so the request log
// can report them. It implements Unwrap so flusher/hijacker capabilities of
// the underlying ResponseWriter remain available.
type traceWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Unwrap exposes the wrapped writer for http.ResponseController and the
// standard middleware that introspect optional interfaces.
func (w *traceWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// WriteHeader records the status before delegating.
func (w *traceWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Write records byte count and defaults the status to 200 on first write.
func (w *traceWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// TraceMiddleware assigns or honors a correlation ID, stores it on the
// request context, echoes it on the response and emits one structured log
// line per request. An inbound X-Request-ID is trusted and reused so traces
// continue across the gateway and proxied products.
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(traceHeader)
		if id == "" {
			id = newTraceID()
		}

		tw := &traceWriter{ResponseWriter: w}
		ctx := WithTraceID(r.Context(), id)
		w.Header().Set(traceHeader, id)

		start := time.Now()
		next.ServeHTTP(tw, r.WithContext(ctx))

		slog.InfoContext(ctx, "http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", tw.status,
			"bytes", tw.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
			"trace_id", id,
		)
	})
}

// RecoverMiddleware converts a handler panic into a 500 error envelope so a
// single fault never crashes the process, and records the stack in logs.
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"error", rec,
					"trace_id", TraceID(r.Context()),
					"stack", string(debug.Stack()),
				)
				WriteError(w, r, http.StatusInternalServerError, "INTERNAL", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// WriteJSON writes v as a JSON body with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the family-wide error envelope:
//
//	{"error":{"code":...,"message":...,"trace_id":...}}
//
// The trace ID is pulled from the request context set by TraceMiddleware.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":     code,
			"message":  message,
			"trace_id": TraceID(r.Context()),
		},
	})
}

// newTraceID returns a 128-bit hex correlation ID.
func newTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// rand.Read failing is not recoverable; fall back to timestamp.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}
