// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Local account authenticator. Selected by auth.mode=local as the
// emergency/break-glass backend: NereidSphere verifies the login form
// credentials against statically configured bcrypt-hashed accounts and
// establishes the same sealed session. Local sessions carry no upstream
// tokens, so the resolver never refreshes them and logout needs no
// revocation.

package auth

import (
	"context"
	"fmt"

	"github.com/NereidAI/NereidSphere/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// LocalAuthenticator checks credentials against config-declared accounts.
type LocalAuthenticator struct {
	users []config.LocalUser
}

// NewLocalAuthenticator builds the local authenticator from config.
func NewLocalAuthenticator(users []config.LocalUser) *LocalAuthenticator {
	return &LocalAuthenticator{users: users}
}

// Authenticate verifies the password against the user's bcrypt hash. A
// constant-time compare is performed by bcrypt even for unknown usernames.
func (a *LocalAuthenticator) Authenticate(_ context.Context, username, password string) (*AuthResult, error) {
	for i := range a.users {
		u := &a.users[i]
		if u.Username != username {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordBcrypt), []byte(password)); err != nil {
			return nil, fmt.Errorf("local password check: %w", err)
		}
		roles := make([]string, len(u.Roles))
		copy(roles, u.Roles)
		return &AuthResult{
			Identity: Identity{
				Sub:      "local:" + u.Username,
				Email:    u.Email,
				Name:     u.Name,
				Username: u.Username,
				Roles:    roles,
			},
		}, nil
	}

	// Run a throwaway comparison so a missing user costs roughly the same
	// as a wrong password, blunting username enumeration via timing.
	_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
	return nil, fmt.Errorf("unknown local user %q", username)
}

// dummyBcryptHash backs the timing equalisation for unknown usernames.
var dummyBcryptHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("nereidsphere-dummy"), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return h
}()
