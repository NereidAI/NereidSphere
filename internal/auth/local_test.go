// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/NereidAI/NereidSphere/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func localTestUsers(t *testing.T) []config.LocalUser {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return []config.LocalUser{{
		Username:       "admin",
		Name:           "Admin",
		Email:          "admin@local",
		PasswordBcrypt: string(hash),
		Roles:          []string{"admin", "user"},
	}}
}

func TestLocalAuthenticatorAcceptsValidPassword(t *testing.T) {
	a := NewLocalAuthenticator(localTestUsers(t))
	res, err := a.Authenticate(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if res.Identity.Sub != "local:admin" || res.Identity.Email != "admin@local" {
		t.Fatalf("unexpected identity: %+v", res.Identity)
	}
	if len(res.Identity.Roles) != 2 || res.AccessToken != "" || !res.AccessExpiry.IsZero() {
		t.Fatalf("local session must carry no upstream tokens: %+v", res)
	}
}

func TestLocalAuthenticatorRejectsBadCredentials(t *testing.T) {
	a := NewLocalAuthenticator(localTestUsers(t))
	if _, err := a.Authenticate(context.Background(), "admin", "wrong"); err == nil {
		t.Fatal("expected wrong password rejected")
	}
	if _, err := a.Authenticate(context.Background(), "ghost", "secret"); err == nil {
		t.Fatal("expected unknown user rejected")
	}
}
