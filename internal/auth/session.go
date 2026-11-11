// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Session storage: the authenticated principal is kept in a sealed, HttpOnly
// cookie. NereidSphere runs no session database, so the layer stays
// horizontally scalable and stateless. The blob is encrypted with
// AES-256-GCM using the configured secret key. Login itself happens
// server-to-server against the OIDC provider, so no short-lived browser
// handshake cookie is required.

package auth

import (
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// sealedPayloadFlate marks a deflate-compressed plaintext inside a sealed
// value. Legacy values start directly with JSON ('{') and are still accepted.
const sealedPayloadFlate byte = 0x01

// SessionCookie is the name of the encrypted session cookie.
const SessionCookie = "ns_session"

// ErrNoSession is returned when no valid session cookie is present.
var ErrNoSession = errors.New("no valid session")

// Identity is the authenticated user surfaced to the gateway and portal.
type Identity struct {
	// Sub is the stable provider subject identifier.
	Sub string `json:"sub"`
	// Email is the verified mailbox when the provider supplies one.
	Email string `json:"email"`
	// Name is the display name.
	Name string `json:"name"`
	// Username is the preferred_username claim.
	Username string `json:"preferred_username"`
	// Roles are the realm roles used for coarse authorization headers.
	Roles []string `json:"roles"`
	// Groups are the realm groups used for fine-grained product tenant mapping.
	Groups []string `json:"groups,omitempty"`
}

// SessionData is the sealed post-login session payload.
type SessionData struct {
	// Identity carries the user claims.
	Identity Identity `json:"identity"`
	// AccessToken is the bearer token used for gateway/userinfo calls.
	AccessToken string `json:"access_token"`
	// IDToken is the OIDC id token, retained as the id_token_hint for
	// RP-initiated logout. Absent for local sessions.
	IDToken string `json:"id_token,omitempty"`
	// RefreshToken renews the access token; may be absent.
	RefreshToken string `json:"refresh_token,omitempty"`
	// AccessExpiry is the upstream access token expiry; the resolver
	// transparently refreshes after this instant.
	AccessExpiry time.Time `json:"access_expiry"`
	// LoginAt is the original authentication instant.
	LoginAt time.Time `json:"login_at"`
	// LastSeen bounds the idle lifetime and is slid on activity.
	LastSeen time.Time `json:"last_seen"`
	// Expiry is the absolute session deadline.
	Expiry time.Time `json:"expiry"`
}

// Codec seals and opens encrypted cookie values with AES-256-GCM.
type Codec struct {
	gcm cipher.AEAD
}

// NewCodec derives a GCM cipher from a 32-byte key.
func NewCodec(secret string) (*Codec, error) {
	key := []byte(secret)
	if len(key) != 32 {
		return nil, fmt.Errorf("session secret must be exactly 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return &Codec{gcm: gcm}, nil
}

// seal encrypts v into a URL-safe string. The JSON payload is deflated
// first: session bundles carry three base64 JWTs whose raw sealed form can
// exceed the browser per-cookie limit of ~4 KB, while base64 text compresses
// to roughly a third of its size.
func (c *Codec) seal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal cookie: %w", err)
	}
	compressed, err := compressPayload(raw)
	if err != nil {
		return "", err
	}
	payload := append([]byte{sealedPayloadFlate}, compressed...)
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := c.gcm.Seal(nonce, nonce, payload, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// open decrypts a value produced by seal into v. Legacy uncompressed blobs
// (plaintext beginning with '{') remain readable.
func (c *Codec) open(value string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return fmt.Errorf("decode cookie: %w", err)
	}
	if len(raw) < c.gcm.NonceSize() {
		return errors.New("cookie too short")
	}
	nonce, ciphertext := raw[:c.gcm.NonceSize()], raw[c.gcm.NonceSize():]
	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decrypt cookie: %w", err)
	}
	if len(plaintext) > 0 && plaintext[0] == sealedPayloadFlate {
		plaintext, err = decompressPayload(plaintext[1:])
		if err != nil {
			return err
		}
	}
	if err := json.Unmarshal(plaintext, v); err != nil {
		return fmt.Errorf("unmarshal cookie: %w", err)
	}
	return nil
}

// compressPayload deflates raw with the best speed; JWT base64 is the
// dominant content and reaches the same ratio at every level.
func compressPayload(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return nil, fmt.Errorf("create compressor: %w", err)
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("compress cookie: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("flush compressor: %w", err)
	}
	return buf.Bytes(), nil
}

// decompressPayload inflates payload produced by compressPayload.
func decompressPayload(payload []byte) ([]byte, error) {
	zr := flate.NewReader(bytes.NewReader(payload))
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("decompress cookie: %w", err)
	}
	if err := zr.Close(); err != nil {
		return nil, fmt.Errorf("close decompressor: %w", err)
	}
	return raw, nil
}

// CookieOptions centralizes the cookie security flags.
type CookieOptions struct {
	// Name overrides the default session cookie name.
	Name string
	// Domain scopes the cookie; empty means the host only.
	Domain string
	// Secure sets the Secure flag.
	Secure bool
	// TTL bounds MaxAge for persistent cookies.
	TTL time.Duration
}

// write sets an encrypted cookie for v.
func (c *Codec) write(w http.ResponseWriter, opts CookieOptions, v any) error {
	value, err := c.seal(v)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     opts.Name,
		Value:    value,
		Path:     "/",
		Domain:   opts.Domain,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(opts.TTL),
		MaxAge:   int(opts.TTL.Seconds()),
	})
	return nil
}

// read opens and decrypts the named cookie into v.
func (c *Codec) read(r *http.Request, name string, v any) error {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ErrNoSession
	}
	return c.open(cookie.Value, v)
}

// clear expires the named cookie.
func clear(w http.ResponseWriter, name, domain string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// SaveSession writes the authenticated session cookie.
func (c *Codec) SaveSession(w http.ResponseWriter, opts CookieOptions, data *SessionData) error {
	if opts.Name == "" {
		opts.Name = SessionCookie
	}
	return c.write(w, opts, data)
}

// LoadSession reads and decrypts the session cookie and rejects sessions
// past their absolute deadline. The idle-timeout check and transparent token
// refresh live in the SessionResolver, which owns the session policy.
func (c *Codec) LoadSession(r *http.Request, name string) (*SessionData, error) {
	if name == "" {
		name = SessionCookie
	}
	var data SessionData
	if err := c.read(r, name, &data); err != nil {
		return nil, err
	}
	if !data.Expiry.IsZero() && time.Now().After(data.Expiry) {
		return nil, ErrNoSession
	}
	return &data, nil
}

// ClearSession expires the session cookie.
func (c *Codec) ClearSession(w http.ResponseWriter, domain string) {
	clear(w, SessionCookie, domain)
}

// RolesHeader renders roles as a single comma-joined header value.
func (s *SessionData) RolesHeader() string {
	return strings.Join(s.Identity.Roles, ",")
}
