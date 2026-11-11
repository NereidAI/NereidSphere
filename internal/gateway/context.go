// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Request-scoped gateway context carrying the authenticated session.

package gateway

import (
	"context"

	"github.com/NereidAI/NereidSphere/internal/auth"
)

// sessionKey is the unexported context key for the authenticated session.
type sessionKey struct{}

// WithSession returns ctx carrying the authenticated session.
func WithSession(ctx context.Context, s *auth.SessionData) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFromContext retrieves the session placed by RequireSession.
func SessionFromContext(ctx context.Context) (*auth.SessionData, bool) {
	s, ok := ctx.Value(sessionKey{}).(*auth.SessionData)
	return s, ok
}
