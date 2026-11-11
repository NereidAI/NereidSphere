// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Claim extraction and session-loading helpers shared by the auth handlers
// and the gateway authentication middleware.

package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

// errInvalidRedirect indicates a post-login destination failed the
// allowlist check.
var errInvalidRedirect = errors.New("redirect URL is not in the product allowlist")

// realmClaims models the subset of the identity service token claims
// NereidSphere consumes. Application roles arrive nested under
// realm_access.roles, the roles-claim convention of the current identity
// engine; see docs/oidc-integration.md.
type realmClaims struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
	// Groups are the realm group paths used for product tenant/role
	// mapping, e.g. /nereidlink/{tenant}/{role}; see docs/oidc-integration.md.
	Groups []string `json:"groups"`
}

// identityFromClaims decodes the verified id token into the portal Identity.
// The id token carries profile claims; roles and suite product groups are read
// separately from the access token (RolesFromAccessToken / GroupsFromAccessToken)
// because the engine's mappers place realm_access and groups reliably on the
// access token.
func identityFromClaims(tok *coreoidc.IDToken) (Identity, error) {
	var claims realmClaims
	if err := tok.Claims(&claims); err != nil {
		return Identity{}, err
	}

	return Identity{
		Sub:      claims.Sub,
		Email:    claims.Email,
		Name:     claims.Name,
		Username: claims.PreferredUsername,
	}, nil
}

// RolesFromAccessToken extracts application roles from an opaque JWT access
// token's realm_access.roles claim. The signature is already trusted
// transitively because the token arrived directly over TLS from the issuer
// during the code exchange; only the payload is decoded here.
func RolesFromAccessToken(accessToken string) []string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims realmClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}

	roles := make([]string, 0, len(claims.RealmAccess.Roles))
	for _, role := range claims.RealmAccess.Roles {
		if isBuiltInRole(role) {
			continue
		}
		roles = append(roles, role)
	}
	return roles
}

// isBuiltInRole reports whether role is an engine-internal role granted
// automatically (offline access, user-managed access or a per-tenant
// default-roles-* composite) rather than a meaningful application role.
func isBuiltInRole(role string) bool {
	if role == "offline_access" || role == "uma_authorization" {
		return true
	}
	return strings.HasPrefix(role, "default-roles-")
}

// suiteGroupPrefix is the namespace of the suite product assignment groups
// owned by internal/admin: /nereid{product}/{tenant}/{role}. It is duplicated
// here as a local constant because internal/admin imports this package, so
// importing admin back would create an import cycle.
const suiteGroupPrefix = "/nereid"

// productGroups keeps only the suite product assignment groups, dropping
// engine defaults such as /user so the identity handed to products and the
// handoff ticket carries just the tenant/role mappings.
func productGroups(groups []string) []string {
	var out []string
	for _, g := range groups {
		if strings.HasPrefix(g, suiteGroupPrefix) {
			out = append(out, g)
		}
	}
	return out
}

// ProductAssigned reports whether the identity carries at least one suite
// assignment group for the given product (/nereid{product}/{tenant}/{role}).
// Group membership is the suite-wide authorization for product entry: an
// account that only exists in the identity service, without a matching
// group, holds a valid NS session but is denied entry to that product. The
// trailing slash makes the product token exact ("forge" cannot match
// "forge-legacy").
func ProductAssigned(groups []string, product string) bool {
	prefix := suiteGroupPrefix + product + "/"
	for _, g := range groups {
		if strings.HasPrefix(g, prefix) {
			return true
		}
	}
	return false
}

// SuiteAdminRole is the realm role whose holders administer the whole
// suite. They implicitly hold administrative access to every product
// without a per-product assignment group, so the bootstrap administrator
// can never be locked out of a freshly deployed suite. "operator" only
// manages the account console and is deliberately not included.
const SuiteAdminRole = "admin"

// IsSuiteAdministrator reports whether the identity carries the suite admin
// realm role.
func IsSuiteAdministrator(roles []string) bool {
	for _, r := range roles {
		if r == SuiteAdminRole {
			return true
		}
	}
	return false
}

// CanEnterProduct is the product-entry authorization: an explicit
// /nereid{product}/... assignment, or the suite administrator role. Product
// callbacks map the latter to their own administrative role (platform
// admin / admin / platform); they must not treat "operator" as privileged.
func CanEnterProduct(groups, roles []string, product string) bool {
	return ProductAssigned(groups, product) || IsSuiteAdministrator(roles)
}

// GroupsFromAccessToken extracts the suite product groups from an opaque JWT
// access token's groups claim, produced by the identity service's group
// membership mapper. As with RolesFromAccessToken the signature is trusted
// transitively because the token arrived directly over TLS from the issuer
// during the code exchange; only the payload is decoded here.
func GroupsFromAccessToken(accessToken string) []string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims realmClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return productGroups(claims.Groups)
}
