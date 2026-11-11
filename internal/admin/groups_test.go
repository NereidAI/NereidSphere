// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"errors"
	"testing"
)

func TestParseProductGroup(t *testing.T) {
	tests := []struct {
		name       string
		group      string
		wantProd   ProductID
		wantTenant string
		wantRole   RoleKey
		wantErr    error
	}{
		{
			name:       "link admin",
			group:      "/nereidlink/tenant-foo/admins",
			wantProd:   ProductLink,
			wantTenant: "tenant-foo",
			wantRole:   LinkRoleAdmin,
		},
		{
			name:       "link engineer",
			group:      "/nereidlink/default/engineers",
			wantProd:   ProductLink,
			wantTenant: "default",
			wantRole:   LinkRoleEngineer,
		},
		{
			name:       "forge admin",
			group:      "/nereidforge/default/admin",
			wantProd:   ProductForge,
			wantTenant: "default",
			wantRole:   ForgeRoleAdmin,
		},
		{
			name:       "forge operator",
			group:      "/nereidforge/tenant-bar/operator",
			wantProd:   ProductForge,
			wantTenant: "tenant-bar",
			wantRole:   ForgeRoleOperator,
		},
		{
			name:       "horizon platform",
			group:      "/nereidhorizon/partner-acme/platform",
			wantProd:   ProductHorizon,
			wantTenant: "partner-acme",
			wantRole:   HorizonRolePlatform,
		},
		{
			name:       "horizon partner",
			group:      "/nereidhorizon/default/partner",
			wantProd:   ProductHorizon,
			wantTenant: "default",
			wantRole:   HorizonRolePartner,
		},
		{
			name:    "invalid prefix",
			group:   "/otherlink/tenant/role",
			wantErr: ErrInvalidGroupFormat,
		},
		{
			name:    "wrong parts count",
			group:   "/nereidlink/tenant",
			wantErr: ErrInvalidGroupFormat,
		},
		{
			name:    "unknown product",
			group:   "/nereidunknown/tenant/role",
			wantErr: ErrUnknownProduct,
		},
		{
			name:    "unknown role for link",
			group:   "/nereidlink/tenant/unknown",
			wantErr: ErrUnknownRole,
		},
		{
			name:    "unknown role for forge",
			group:   "/nereidforge/tenant/unknown",
			wantErr: ErrUnknownRole,
		},
		{
			name:    "empty group",
			group:   "",
			wantErr: ErrInvalidGroupFormat,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prod, tenant, role, err := ParseProductGroup(tc.group)
			if tc.wantErr != nil {
				if err == nil || !errors.Is(err, tc.wantErr) {
					t.Errorf("ParseProductGroup(%q) = (%v, %v, %v, %v); want error %v", tc.group, prod, tenant, role, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseProductGroup(%q) unexpected error: %v", tc.group, err)
				return
			}
			if prod != tc.wantProd || tenant != tc.wantTenant || role != tc.wantRole {
				t.Errorf("ParseProductGroup(%q) = (%v, %v, %v); want (%v, %v, %v)", tc.group, prod, tenant, role, tc.wantProd, tc.wantTenant, tc.wantRole)
			}
		})
	}
}

func TestBuildProductGroup(t *testing.T) {
	tests := []struct {
		name      string
		product   ProductID
		tenant    string
		role      RoleKey
		wantGroup string
	}{
		{"link admin", ProductLink, "tenant-foo", LinkRoleAdmin, "/nereidlink/tenant-foo/admins"},
		{"forge operator", ProductForge, "tenant-bar", ForgeRoleOperator, "/nereidforge/tenant-bar/operator"},
		{"horizon partner", ProductHorizon, "partner-acme", HorizonRolePartner, "/nereidhorizon/partner-acme/partner"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildProductGroup(tc.product, tc.tenant, tc.role)
			if got != tc.wantGroup {
				t.Errorf("BuildProductGroup(%v, %q, %v) = %q; want %q", tc.product, tc.tenant, tc.role, got, tc.wantGroup)
			}
		})
	}
}

func TestParseUserGroups(t *testing.T) {
	groups := []string{
		"/nereidlink/tenant-foo/admins",
		"/nereidlink/tenant-bar/engineers",
		"/nereidforge/default/admin",
		"/nereidhorizon/partner-acme/platform",
		"/some-other-group", // should be ignored
	}
	parsed := ParseUserGroups(groups)

	if len(parsed[ProductLink]) != 2 {
		t.Errorf("Link: got %d pairs, want 2", len(parsed[ProductLink]))
	}
	if len(parsed[ProductForge]) != 1 {
		t.Errorf("Forge: got %d pairs, want 1", len(parsed[ProductForge]))
	}
	if len(parsed[ProductHorizon]) != 1 {
		t.Errorf("Horizon: got %d pairs, want 1", len(parsed[ProductHorizon]))
	}
	// Check specific values
	found := false
	for _, p := range parsed[ProductLink] {
		if p.TenantID == "tenant-foo" && p.Role == LinkRoleAdmin {
			found = true
		}
	}
	if !found {
		t.Error("Link tenant-foo/admins not found in parsed")
	}
}

func TestDiffGroups(t *testing.T) {
	current := []string{
		"/nereidlink/tenant-foo/admins",
		"/nereidforge/default/admin",
	}
	target := []string{
		"/nereidlink/tenant-bar/engineers",
		"/nereidforge/default/admin",           // unchanged
		"/nereidhorizon/partner-acme/platform", // new
	}
	toAdd, toRemove := DiffGroups(current, target)

	if len(toAdd) != 2 {
		t.Errorf("toAdd: got %d, want 2; %v", len(toAdd), toAdd)
	}
	if len(toRemove) != 1 {
		t.Errorf("toRemove: got %d, want 1; %v", len(toRemove), toRemove)
	}
	// Check contents
	addSet := make(map[string]bool)
	for _, g := range toAdd {
		addSet[g] = true
	}
	if !addSet["/nereidlink/tenant-bar/engineers"] || !addSet["/nereidhorizon/partner-acme/platform"] {
		t.Errorf("toAdd missing expected groups: %v", toAdd)
	}
	removeSet := make(map[string]bool)
	for _, g := range toRemove {
		removeSet[g] = true
	}
	if !removeSet["/nereidlink/tenant-foo/admins"] {
		t.Errorf("toRemove missing expected group: %v", toRemove)
	}
}
