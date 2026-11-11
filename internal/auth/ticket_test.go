// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingReplayStore struct{}

func (failingReplayStore) Claim(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("store unavailable")
}

func (failingReplayStore) Ready(context.Context) error { return errors.New("store unavailable") }

// newTestTicketStore builds a sealed ticket store for tests.
func newTestTicketStore(t *testing.T, ttl time.Duration) *TicketStore {
	t.Helper()
	codec, err := NewCodec(testSecret)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	return NewTicketStore(codec, ttl)
}

// TestTicketIssueConsume verifies a fresh ticket exchanges once.
func TestTicketIssueConsume(t *testing.T) {
	store := newTestTicketStore(t, time.Minute)
	ticket, err := store.Issue("link", Identity{Sub: "u1"}, "/dashboard")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := store.Consume(t.Context(), ticket.ID, "link")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.Identity.Sub != "u1" || got.To != "/dashboard" || got.Audience != "link" {
		t.Fatalf("unexpected ticket payload: %+v", got)
	}
}

// TestTicketReplay verifies a ticket cannot be exchanged twice.
func TestTicketReplay(t *testing.T) {
	store := newTestTicketStore(t, time.Minute)
	ticket, _ := store.Issue("link", Identity{}, "")
	if _, err := store.Consume(t.Context(), ticket.ID, "link"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := store.Consume(t.Context(), ticket.ID, "link"); err != ErrTicketNotFound {
		t.Fatalf("replay err = %v, want ErrTicketNotFound", err)
	}
}

// TestTicketAudienceMismatch verifies a ticket is rejected (and burned) when
// another product tries to consume it.
func TestTicketAudienceMismatch(t *testing.T) {
	store := newTestTicketStore(t, time.Minute)
	ticket, _ := store.Issue("link", Identity{}, "")
	if _, err := store.Consume(t.Context(), ticket.ID, "forge"); err != ErrTicketAudience {
		t.Fatalf("err = %v, want ErrTicketAudience", err)
	}
	// Burned even on audience failure: the real audience cannot redeem it.
	if _, err := store.Consume(t.Context(), ticket.ID, "link"); err != ErrTicketNotFound {
		t.Fatalf("ticket must be burned after failed consume, got %v", err)
	}
}

// TestTicketExpired verifies TTL enforcement.
func TestTicketExpired(t *testing.T) {
	store := newTestTicketStore(t, 10*time.Millisecond)
	ticket, _ := store.Issue("link", Identity{}, "")
	time.Sleep(15 * time.Millisecond)
	if _, err := store.Consume(t.Context(), ticket.ID, "link"); err != ErrTicketExpired {
		t.Fatalf("err = %v, want ErrTicketExpired", err)
	}
}

// TestTicketUnknown verifies malformed ciphertext is rejected as invalid.
func TestTicketUnknown(t *testing.T) {
	store := newTestTicketStore(t, time.Minute)
	if _, err := store.Consume(t.Context(), "does-not-exist", "link"); err != ErrTicketInvalid {
		t.Fatalf("err = %v, want ErrTicketInvalid", err)
	}
}

func TestTicketReplayStoreFailureFailsClosed(t *testing.T) {
	codec, err := NewCodec(testSecret)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	store := NewTicketStoreWithReplay(codec, time.Minute, failingReplayStore{})
	ticket, err := store.Issue("link", Identity{}, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_, err = store.Consume(t.Context(), ticket.ID, "link")
	if !errors.Is(err, ErrReplayUnavailable) {
		t.Fatalf("err = %v, want ErrReplayUnavailable", err)
	}
}

// TestTicketSealedAndStateless verifies the ticket carries its own encrypted
// payload (no server map) and cannot be read as plaintext JSON.
func TestTicketSealedAndStateless(t *testing.T) {
	store := newTestTicketStore(t, time.Minute)
	ticket, err := store.Issue("link", Identity{Sub: "secret-sub"}, "/x")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if ticket.ID == "" || strings.Contains(ticket.ID, "secret-sub") {
		t.Fatalf("ticket must be sealed ciphertext, got %q", ticket.ID)
	}

	// A different secret key must not open the ticket: this is what makes
	// tickets safe to mint/verify across replicas sharing the session key.
	otherCodec, _ := NewCodec("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	other := NewTicketStore(otherCodec, time.Minute)
	if _, err := other.Consume(t.Context(), ticket.ID, "link"); err == nil {
		t.Fatal("ticket must not decrypt under a different key")
	}
}
