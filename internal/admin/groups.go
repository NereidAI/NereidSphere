// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package admin provides shared utilities for the suite admin console.
package admin

import (
	"errors"
	"strings"
)

// ProductID identifies a suite product for group naming.
type ProductID string

const (
	ProductLink    ProductID = "link"
	ProductForge   ProductID = "forge"
	ProductHorizon ProductID = "horizon"
)

// RoleKey is the role identifier used in group names (maps to product's role_map).
type RoleKey string

const (
	// Link roles (map to NL built-in roles via oidc.groupRole / sso.role_map)
	LinkRoleAdmin    RoleKey = "admins"
	LinkRoleEngineer RoleKey = "engineers"
	LinkRoleReadOnly RoleKey = "read_only"

	// Forge roles (map via config.sso.role_map)
	ForgeRoleAdmin    RoleKey = "admin"
	ForgeRoleOperator RoleKey = "operator"
	ForgeRoleUser     RoleKey = "user"

	// Horizon roles (map via platformGroup / partnerGroupPrefix)
	HorizonRolePlatform RoleKey = "platform"
	HorizonRolePartner  RoleKey = "partner"
)

// Group naming convention: /nereid{product}/{tenant_id}/{role}
const groupPrefix = "/nereid"

var (
	ErrInvalidGroupFormat = errors.New("invalid group format, expected /nereid{product}/{tenant}/{role}")
	ErrUnknownProduct     = errors.New("unknown product in group")
	ErrUnknownRole        = errors.New("unknown role in group")
)

// ParseProductGroup parses a NI group name into product, tenant, role.
// Returns (product, tenant_id, role, nil) on success.
func ParseProductGroup(group string) (ProductID, string, RoleKey, error) {
	if !strings.HasPrefix(group, groupPrefix) {
		return "", "", "", ErrInvalidGroupFormat
	}
	// Strip prefix: /nereid{product}/{tenant}/{role}
	rest := strings.TrimPrefix(group, groupPrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return "", "", "", ErrInvalidGroupFormat
	}
	product := ProductID(parts[0])
	tenant := parts[1]
	role := RoleKey(parts[2])

	// Validate product
	switch product {
	case ProductLink, ProductForge, ProductHorizon:
	default:
		return "", "", "", ErrUnknownProduct
	}

	// Validate role per product
	if !validRole(product, role) {
		return "", "", "", ErrUnknownRole
	}

	return product, tenant, role, nil
}

// BuildProductGroup constructs a NI group name from product, tenant, role.
func BuildProductGroup(product ProductID, tenant string, role RoleKey) string {
	if !validRole(product, role) {
		// Should not happen if caller validates; fallback to raw.
		return groupPrefix + string(product) + "/" + tenant + "/" + string(role)
	}
	return groupPrefix + string(product) + "/" + tenant + "/" + string(role)
}

// validRole checks if a role key is known for the given product.
func validRole(product ProductID, role RoleKey) bool {
	switch product {
	case ProductLink:
		return role == LinkRoleAdmin || role == LinkRoleEngineer || role == LinkRoleReadOnly
	case ProductForge:
		return role == ForgeRoleAdmin || role == ForgeRoleOperator || role == ForgeRoleUser
	case ProductHorizon:
		return role == HorizonRolePlatform || role == HorizonRolePartner
	default:
		return false
	}
}

// ProductRoleMap holds the role keys available for a product (for UI rendering).
type ProductRoleMap struct {
	Product ProductID
	Roles   []RoleKey
}

// AllProductRoleMaps returns the complete mapping for all products.
func AllProductRoleMaps() []ProductRoleMap {
	return []ProductRoleMap{
		{Product: ProductLink, Roles: []RoleKey{LinkRoleAdmin, LinkRoleEngineer, LinkRoleReadOnly}},
		{Product: ProductForge, Roles: []RoleKey{ForgeRoleAdmin, ForgeRoleOperator, ForgeRoleUser}},
		{Product: ProductHorizon, Roles: []RoleKey{HorizonRolePlatform, HorizonRolePartner}},
	}
}

// RolesForProduct returns the selectable role keys for a product id, and
// whether the product participates in the suite product-role convention.
func RolesForProduct(id string) ([]RoleKey, bool) {
	for _, m := range AllProductRoleMaps() {
		if string(m.Product) == id {
			return m.Roles, true
		}
	}
	return nil, false
}

// TenantRolePair represents a tenant+role assignment for a product.
type TenantRolePair struct {
	TenantID string
	Role     RoleKey
}

// ProductTenantRoles maps product -> list of (tenant, role).
type ProductTenantRoles map[ProductID][]TenantRolePair

// ParseUserGroups extracts product->tenant+roles from a user's NI groups.
func ParseUserGroups(groups []string) ProductTenantRoles {
	result := make(ProductTenantRoles)
	for _, g := range groups {
		product, tenant, role, err := ParseProductGroup(g)
		if err != nil {
			continue // ignore non-suite groups
		}
		result[product] = append(result[product], TenantRolePair{TenantID: tenant, Role: role})
	}
	return result
}

// BuildTargetGroups builds the full target group set from desired assignments.
func BuildTargetGroups(assignments ProductTenantRoles) []string {
	var groups []string
	for product, pairs := range assignments {
		for _, p := range pairs {
			groups = append(groups, BuildProductGroup(product, p.TenantID, p.Role))
		}
	}
	return groups
}

// DiffGroups computes groups to add and remove to reach target from current.
func DiffGroups(currentGroups, targetGroups []string) (toAdd, toRemove []string) {
	currentSet := make(map[string]struct{}, len(currentGroups))
	for _, g := range currentGroups {
		currentSet[g] = struct{}{}
	}
	targetSet := make(map[string]struct{}, len(targetGroups))
	for _, g := range targetGroups {
		targetSet[g] = struct{}{}
	}
	for g := range targetSet {
		if _, ok := currentSet[g]; !ok {
			toAdd = append(toAdd, g)
		}
	}
	for g := range currentSet {
		if _, ok := targetSet[g]; !ok {
			toRemove = append(toRemove, g)
		}
	}
	return toAdd, toRemove
}
