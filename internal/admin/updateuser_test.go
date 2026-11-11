// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestUpdateUserOmitsEmptyUsername guards the KC26 failure where a profile
// edit PUT carried an explicit "username":"" (the zero value of the shared
// keycloakUser struct, whose tag lacked omitempty). Keycloak answers such a
// body with 400 {"errorMessage":"User name is missing"}, surfacing in the
// console as a 502 IDENTITY_ERROR on every profile save.
func TestUpdateUserOmitsEmptyUsername(t *testing.T) {
	var received map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/nereidai/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "header." + encode(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}) + ".sig",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/admin/realms/nereidai/users/u1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &received)
		// Mirror Keycloak 26: an explicitly empty username is rejected.
		if username, present := received["username"]; present && username == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessage":"User name is missing"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL+"/realms/nereidai", "nereidsphere-admin", "secret")
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	email := "alice@example.com"
	first := "Alice"
	last := "Example"
	if err := client.UpdateUser(t.Context(), "u1", UpdateUserRequest{
		Email:     &email,
		FirstName: &first,
		LastName:  &last,
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, present := received["username"]; present {
		t.Fatalf("profile update must not send username, body=%v", received)
	}
	if received["email"] != email || received["firstName"] != first || received["lastName"] != last {
		t.Fatalf("profile fields not forwarded: %v", received)
	}
}
