// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"sync"
	"time"
)

// ReplayStore atomically records one-time ticket JTIs. Claim returns true
// only for the first successful observation of key. Store failures must be
// handled fail-closed by ticket verification.
type ReplayStore interface {
	Claim(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Ready(ctx context.Context) error
}

// MemoryReplayStore is the development store. It is correct for one process
// but cannot enforce single-use across multiple NS replicas.
type MemoryReplayStore struct {
	mu       sync.Mutex
	consumed map[string]time.Time
}

// NewMemoryReplayStore builds an in-process replay store.
func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{consumed: make(map[string]time.Time)}
}

// Claim records key for ttl and reports whether it was not already present.
func (s *MemoryReplayStore) Claim(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for usedKey, expiresAt := range s.consumed {
		if now.After(expiresAt) {
			delete(s.consumed, usedKey)
		}
	}
	if _, used := s.consumed[key]; used {
		return false, nil
	}
	if len(s.consumed) >= maxReplayJTIs {
		for usedKey := range s.consumed {
			delete(s.consumed, usedKey)
			break
		}
	}
	s.consumed[key] = now.Add(ttl)
	return true, nil
}

// Ready is always true for the process-local store.
func (s *MemoryReplayStore) Ready(context.Context) error { return nil }
