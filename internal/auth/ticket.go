// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// One-time cross-product SSO handoff tickets.
//
// The browser never talks to the identity service, so jumping to an
// independently hosted product cannot rely on an IdP cookie. Instead NS acts
// as the suite hub: when an authenticated user enters a product, NS mints a
// short-lived, single-use ticket bound to that product and redirects the
// browser to the product callback. The product backend immediately exchanges
// the ticket server to server for the verified identity (see ticket verify
// endpoint), then establishes its own local session.
//
// Tickets are stateless: the payload is sealed with the same AES-256-GCM
// session key, so any NS replica can verify a ticket another replica minted.
// Single-use is enforced with a replay store. Process memory is valid for
// single-replica development; multi-replica deployments inject shared Valkey.

package auth

import (
	"context"
	"errors"
	"time"
)

// TicketTTL bounds how long a handoff may sit in a redirect before exchange.
const TicketTTL = 30 * time.Second

// maxReplayJTIs bounds the single-use cache; at normal traffic it holds far
// fewer entries than this because every entry self-expires after TicketTTL.
const maxReplayJTIs = 10000

// ticketKind binds the sealed blob to the handoff purpose so a session or
// flow cookie ciphertext cannot be replayed as a ticket.
const ticketKind = "ns-handoff-ticket-v1"

// Ticket errors. Callers map them onto the standard error envelope.
var (
	ErrTicketNotFound    = errors.New("ticket not found or already used")
	ErrTicketExpired     = errors.New("ticket expired")
	ErrTicketAudience    = errors.New("ticket was not issued for this product")
	ErrTicketInvalid     = errors.New("ticket is invalid")
	ErrReplayUnavailable = errors.New("ticket replay store is unavailable")
)

// Ticket is the verified handoff credential.
type Ticket struct {
	// ID is the sealed, URL-safe ticket string sent to the product.
	ID string
	// JTI is the unique per-ticket identifier used for replay defence.
	JTI string
	// Audience is the product id permitted to consume the ticket.
	Audience string
	// Identity is the authenticated principal handed to the product.
	Identity Identity
	// To is the allowlist-relative deep link inside the product.
	To string
	// ExpiresAt bounds the ticket lifetime.
	ExpiresAt time.Time
}

// sealedTicket is the AEAD-encrypted ticket envelope.
type sealedTicket struct {
	Kind      string    `json:"kind"`
	JTI       string    `json:"jti"`
	Audience  string    `json:"aud"`
	Identity  Identity  `json:"identity"`
	To        string    `json:"to"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

// TicketStore seals tickets and tracks consumed JTIs for single-use
// semantics. It is safe for concurrent use.
type TicketStore struct {
	codec  *Codec
	ttl    time.Duration
	replay ReplayStore
}

// NewTicketStore builds a ticket issuer/verifier using the process-local
// replay store. It is intended for development and single-replica setups.
func NewTicketStore(codec *Codec, ttl time.Duration) *TicketStore {
	return NewTicketStoreWithReplay(codec, ttl, NewMemoryReplayStore())
}

// NewTicketStoreWithReplay injects a shared replay store for multi-replica
// deployments.
func NewTicketStoreWithReplay(codec *Codec, ttl time.Duration, replay ReplayStore) *TicketStore {
	if replay == nil {
		replay = NewMemoryReplayStore()
	}
	return &TicketStore{codec: codec, ttl: ttl, replay: replay}
}

// ReplayStore exposes the configured single-use claim store.
func (s *TicketStore) ReplayStore() ReplayStore { return s.replay }

// Issue seals a single-use ticket for the product and deep link.
func (s *TicketStore) Issue(productID string, identity Identity, to string) (*Ticket, error) {
	jti, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	envelope := sealedTicket{
		Kind:      ticketKind,
		JTI:       jti,
		Audience:  productID,
		Identity:  identity,
		To:        to,
		IssuedAt:  now,
		ExpiresAt: now.Add(s.ttl),
	}
	sealed, err := s.codec.seal(envelope)
	if err != nil {
		return nil, err
	}
	return &Ticket{
		ID:        sealed,
		JTI:       jti,
		Audience:  productID,
		Identity:  identity,
		To:        to,
		ExpiresAt: envelope.ExpiresAt,
	}, nil
}

// Consume decrypts the ticket, validates purpose, audience and expiry, and
// atomically records its JTI so it can never be exchanged twice.
func (s *TicketStore) Consume(ctx context.Context, sealed, audience string) (*Ticket, error) {
	var envelope sealedTicket
	if err := s.codec.open(sealed, &envelope); err != nil {
		return nil, ErrTicketInvalid
	}
	if envelope.Kind != ticketKind {
		return nil, ErrTicketInvalid
	}
	if time.Now().After(envelope.ExpiresAt) {
		return nil, ErrTicketExpired
	}
	// Record consumption before the audience check so a rejected exchange
	// still burns the ticket, preventing audience probing with one ticket.
	claimed, err := s.replay.Claim(ctx, replayKey(envelope.Audience, envelope.JTI), time.Until(envelope.ExpiresAt))
	if err != nil {
		return nil, errors.Join(ErrReplayUnavailable, err)
	}
	if !claimed {
		return nil, ErrTicketNotFound
	}
	if envelope.Audience != audience {
		return nil, ErrTicketAudience
	}
	return &Ticket{
		ID:        sealed,
		JTI:       envelope.JTI,
		Audience:  envelope.Audience,
		Identity:  envelope.Identity,
		To:        envelope.To,
		ExpiresAt: envelope.ExpiresAt,
	}, nil
}

func replayKey(audience, jti string) string {
	return "ns:ticket:v1:" + audience + ":" + jti
}
