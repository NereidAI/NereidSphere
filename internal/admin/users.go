// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"strings"
)

var SuiteRoles = []string{"user", "operator", "admin"}

type User struct {
	ID                 string   `json:"id"`
	Username           string   `json:"username"`
	Email              string   `json:"email,omitempty"`
	FirstName          string   `json:"first_name,omitempty"`
	LastName           string   `json:"last_name,omitempty"`
	Enabled            bool     `json:"enabled"`
	EmailVerified      bool     `json:"email_verified"`
	Federated          bool     `json:"federated"`
	FederationProvider string   `json:"federation_provider,omitempty"`
	RequiredActions    []string `json:"required_actions"`
	Roles              []string `json:"roles"`
	CreatedAt          int64    `json:"created_at,omitempty"`
}

type UserPage struct {
	Users []User `json:"users"`
	Total int    `json:"total"`
	First int    `json:"first"`
	Max   int    `json:"max"`
}

type keycloakUser struct {
	ID                   string                  `json:"id,omitempty"`
	Username             string                  `json:"username,omitempty"`
	Email                string                  `json:"email,omitempty"`
	FirstName            string                  `json:"firstName,omitempty"`
	LastName             string                  `json:"lastName,omitempty"`
	Enabled              *bool                   `json:"enabled,omitempty"`
	EmailVerified        bool                    `json:"emailVerified,omitempty"`
	FederationLink       string                  `json:"federationLink,omitempty"`
	FederationIdentities []keycloakFederatedLink `json:"federationIdentities,omitempty"`
	Credentials          []keycloakCredential    `json:"credentials,omitempty"`
	RequiredActions      []string                `json:"requiredActions,omitempty"`
	CreatedTimestamp     int64                   `json:"createdTimestamp,omitempty"`
}

type keycloakFederatedLink struct {
	IdentityProvider string `json:"identityProvider"`
	UserName         string `json:"userName"`
}

type keycloakCredential struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Temporary bool   `json:"temporary"`
}

type CreateUserRequest struct {
	Username     string                             `json:"username"`
	Email        string                             `json:"email"`
	FirstName    string                             `json:"first_name"`
	LastName     string                             `json:"last_name"`
	Password     string                             `json:"password"`
	Temporary    bool                               `json:"temporary"`
	Roles        []string                           `json:"roles"`
	ProductRoles map[string][]ProductRoleAssignment `json:"product_roles,omitempty"`
}

type UpdateUserRequest struct {
	Email     *string `json:"email"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

// --- Client methods (defined in client.go) ---

// ListUsers returns a page of users.
// CreateUser creates a new user.
// GetUser retrieves a user by ID.
// UpdateUser updates user fields.
// SetEnabled enables/disables a user.
// DeleteUser deletes a user.
// ResetPassword resets a user's password.
// SetRoles sets a user's realm roles.
// EnabledAdminCount returns the number of enabled admin users.
// IsFederated checks if a user is federated.
// These methods are defined in client.go.

// --- Helper functions ---

func projectUser(u *keycloakUser) User {
	out := User{
		ID:              u.ID,
		Username:        u.Username,
		Email:           u.Email,
		FirstName:       u.FirstName,
		LastName:        u.LastName,
		Enabled:         u.Enabled != nil && *u.Enabled,
		EmailVerified:   u.EmailVerified,
		RequiredActions: append([]string{}, u.RequiredActions...),
		Roles:           []string{},
		CreatedAt:       u.CreatedTimestamp,
	}
	if u.FederationLink != "" {
		out.Federated = true
		out.FederationProvider = providerFromLink(u.FederationLink)
	}
	if len(u.FederationIdentities) > 0 {
		out.Federated = true
		if out.FederationProvider == "" {
			out.FederationProvider = u.FederationIdentities[0].IdentityProvider
		}
	}
	return out
}

func providerFromLink(link string) string {
	for _, marker := range []string{"/broker/", "broker/"} {
		if idx := strings.Index(link, marker); idx >= 0 {
			rest := link[idx+len(marker):]
			if slash := strings.Index(rest, "/"); slash >= 0 {
				return rest[:slash]
			}
			return rest
		}
	}
	return link
}

func createdUserID(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if idx := strings.Index(location, "/users/"); idx >= 0 {
		rest := location[idx+len("/users/"):]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			return rest[:slash]
		}
		return rest
	}
	return ""
}
