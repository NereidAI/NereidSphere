// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import "testing"

// TestListUsersShowsBaselineUserRole pins the effective-role view: `user`
// is granted to every account through the realm default role
// (default-roles-... composite), so GET /roles/user/users — direct members
// only — returns nobody and the list column looked empty even after an
// admin "saved" the user role (SetRoles diffed against composite roles and
// the inherited baseline made the write a no-op).
func TestListUsersShowsBaselineUserRole(t *testing.T) {
	_, client := newMockAdmin(t)
	page, err := client.ListUsers(t.Context(), "", 0, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	indexed := map[string]User{}
	for _, u := range page.Users {
		indexed[u.ID] = u
	}

	// The mock grants direct "admin" only to u1; both users must still
	// show the implicit baseline.
	roles := indexed["u1"].Roles
	if len(roles) != 2 || roles[0] != "user" || roles[1] != "admin" {
		t.Fatalf("u1 roles = %v, want [user admin]", roles)
	}
	fed := indexed["fed1"].Roles
	if len(fed) != 1 || fed[0] != "user" {
		t.Fatalf("fed1 roles = %v, want baseline [user]", fed)
	}
}

// TestSetRolesUserBaselineIsIdempotent documents that saving only the
// baseline role succeeds without needing a direct mapping: the inherited
// default role already satisfies the desired state.
func TestSetRolesUserBaselineIsIdempotent(t *testing.T) {
	m, client := newMockAdmin(t)
	// u1 has no direct suite-role mappings in the mock's composite view.
	if err := client.SetRoles(t.Context(), "u2", []string{"user"}); err != nil {
		t.Fatalf("set baseline role: %v", err)
	}
	for _, call := range m.calls {
		if call == "POST /admin/realms/nereidai/users/u2/role-mappings/realm" ||
			call == "DELETE /admin/realms/nereidai/users/u2/role-mappings/realm" {
			t.Fatalf("baseline user role needs no direct mapping write, got %q", call)
		}
	}
}

// TestSetRolesDemoteRemovesPrivilege verifies that saving the baseline role
// removes a previously direct admin/operator grant, while user itself is
// never sent (it stays inherited).
func TestSetRolesDemoteRemovesPrivilege(t *testing.T) {
	m, client := newMockAdmin(t) // u1 composite: user + direct admin
	if err := client.SetRoles(t.Context(), "u1", []string{"user"}); err != nil {
		t.Fatalf("demote: %v", err)
	}
	var sawDelete bool
	for _, call := range m.calls {
		if call == "DELETE /admin/realms/nereidai/users/u1/role-mappings/realm" {
			sawDelete = true
		}
		if call == "POST /admin/realms/nereidai/users/u1/role-mappings/realm" {
			t.Fatalf("demotion must not add mappings: %q", call)
		}
	}
	if !sawDelete {
		t.Fatalf("demotion must remove the direct admin mapping; calls=%v", m.calls)
	}
}

// TestSetRolesIdempotentOnExistingGrants verifies saving the same effective
// roles performs no writes.
func TestSetRolesIdempotentOnExistingGrants(t *testing.T) {
	m, client := newMockAdmin(t)
	if err := client.SetRoles(t.Context(), "u1", []string{"user", "admin"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	for _, call := range m.calls {
		if call == "POST /admin/realms/nereidai/users/u1/role-mappings/realm" ||
			call == "DELETE /admin/realms/nereidai/users/u1/role-mappings/realm" {
			t.Fatalf("identical role set must be a no-op, got %q", call)
		}
	}
}
