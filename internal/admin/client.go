// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package admin provides NI Admin REST client for user management.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is the NI Admin REST client using the nereidsphere-admin service account.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenCache   struct {
		token     string
		expiresAt time.Time
	}
	roleIndexCache struct {
		index     map[string]realmRole
		expiresAt time.Time
	}
}

// NewClient creates a new NI Admin REST client.
func NewClient(baseURL, clientID, clientSecret string) (*Client, error) {
	if baseURL == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("issuer, clientID, and clientSecret are required")
	}
	return &Client{
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ensureToken fetches a valid access token via client_credentials.
func (c *Client) ensureToken(ctx context.Context) error {
	if time.Now().Before(c.tokenCache.expiresAt.Add(-30 * time.Second)) {
		return nil // still valid
	}
	tokenURL := c.baseURL + "/protocol/openid-connect/token"
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	resp, err := c.httpClient.PostForm(tokenURL, data)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request status %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	c.tokenCache.token = tr.AccessToken
	c.tokenCache.expiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return nil
}

// adminBase converts the issuer URL into the realm Admin REST base URL
// (issuer ".../realms/nereidai" -> ".../admin/realms/nereidai").
func adminBase(issuer string) string {
	issuer = strings.TrimSuffix(issuer, "/")
	if i := strings.Index(issuer, "/realms/"); i >= 0 {
		return issuer[:i] + "/admin/realms/nereidai"
	}
	return issuer + "/admin/realms/nereidai"
}

// do executes a request with JSON body and returns status, body, error.
func (c *Client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	if err := c.ensureToken(ctx); err != nil {
		return 0, nil, err
	}
	fullURL := adminBase(c.baseURL) + path
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenCache.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

// doWithHeaders executes a request and returns status, body, headers.
func (c *Client) doWithHeaders(ctx context.Context, method, path string, body any) (int, []byte, http.Header, error) {
	if err := c.ensureToken(ctx); err != nil {
		return 0, nil, nil, err
	}
	fullURL := adminBase(c.baseURL) + path
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenCache.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, err
	}
	return resp.StatusCode, respBody, resp.Header, nil
}

type realmRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListUsers(ctx context.Context, search string, first, max int) (*UserPage, error) {
	path := "/users?first=" + strconv.Itoa(first) + "&max=" + strconv.Itoa(max)
	if search != "" {
		path += "&search=" + url.QueryEscape(search)
	}
	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	var kcUsers []keycloakUser
	if err := json.Unmarshal(body, &kcUsers); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}

	total, err := c.CountUsers(ctx, search)
	if err != nil {
		return nil, err
	}
	roleMembers, err := c.suiteRoleMembers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]User, 0, len(kcUsers))
	for i := range kcUsers {
		u := projectUser(&kcUsers[i])
		u.Roles = effectiveSuiteRoles(roleMembers[u.ID])
		users = append(users, u)
	}
	return &UserPage{Users: users, Total: total, First: first, Max: max}, nil
}

// BaselineSuiteRole is granted to every account through the realm default
// role (default-roles-... composite), so GET /roles/user/users — which lists
// direct members only — never returns ordinary accounts even though every
// access token carries it. It is therefore rendered as an implicit baseline
// rather than a direct membership.
const BaselineSuiteRole = "user"

// effectiveSuiteRoles merges direct suite-role memberships with the implicit
// baseline role and returns them in SuiteRoles order so the console shows
// what an account can actually do, not only direct mappings.
func effectiveSuiteRoles(direct []string) []string {
	have := make(map[string]bool, len(SuiteRoles))
	for _, r := range direct {
		if isSuiteRole(r) {
			have[r] = true
		}
	}
	have[BaselineSuiteRole] = true
	out := make([]string, 0, len(SuiteRoles))
	for _, r := range SuiteRoles {
		if have[r] {
			out = append(out, r)
		}
	}
	return out
}

func (c *Client) CountUsers(ctx context.Context, search string) (int, error) {
	path := "/users/count"
	if search != "" {
		path += "?search=" + url.QueryEscape(search)
	}
	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, &UpstreamError{Status: status, Body: string(body)}
	}
	var total int
	if err := json.Unmarshal(body, &total); err != nil {
		return 0, fmt.Errorf("decode user count: %w", err)
	}
	return total, nil
}

func (c *Client) GetUser(ctx context.Context, id string) (*User, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	var kcUser keycloakUser
	if err := json.Unmarshal(body, &kcUser); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	u := projectUser(&kcUser)
	roles, err := c.suiteRoles(ctx, id)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return &u, nil
}

func (c *Client) IsFederated(ctx context.Context, id string) (bool, error) {
	u, err := c.GetUser(ctx, id)
	if err != nil {
		return false, err
	}
	return u.Federated, nil
}

func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (string, error) {
	enabled := true
	kc := keycloakUser{
		Username:  req.Username,
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Enabled:   &enabled,
	}
	status, body, headers, err := c.doWithHeaders(ctx, http.MethodPost, "/users", kc)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated {
		return "", &UpstreamError{Status: status, Body: string(body)}
	}

	created := createdUserID(headers.Get("Location"))
	if created == "" {
		created, err = c.findUserID(ctx, req.Username)
		if err != nil {
			return "", err
		}
	}
	if err := c.SetRoles(ctx, created, req.Roles); err != nil {
		if cleanupErr := c.DeleteUser(context.WithoutCancel(ctx), created); cleanupErr != nil {
			slog.ErrorContext(ctx, "failed to remove partially created user", "user_id", created, "error", cleanupErr)
		}
		return "", err
	}
	if req.Password != "" {
		if err := c.ResetPassword(ctx, created, req.Password, req.Temporary); err != nil {
			if cleanupErr := c.DeleteUser(context.WithoutCancel(ctx), created); cleanupErr != nil {
				slog.ErrorContext(ctx, "failed to remove partially created user", "user_id", created, "error", cleanupErr)
			}
			return "", err
		}
	}
	return created, nil
}

func (c *Client) UpdateUser(ctx context.Context, id string, req UpdateUserRequest) error {
	body := keycloakUser{}
	if req.Email != nil {
		body.Email = *req.Email
	}
	if req.FirstName != nil {
		body.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		body.LastName = *req.LastName
	}
	status, raw, err := c.do(ctx, http.MethodPut, "/users/"+url.PathEscape(id), body)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return &UpstreamError{Status: status, Body: string(raw)}
	}
	return nil
}

func (c *Client) SetEnabled(ctx context.Context, id string, enabled bool) error {
	body := map[string]any{"enabled": enabled}
	status, raw, err := c.do(ctx, http.MethodPut, "/users/"+url.PathEscape(id), body)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return &UpstreamError{Status: status, Body: string(raw)}
	}
	return nil
}

func (c *Client) DeleteUser(ctx context.Context, id string) error {
	status, raw, err := c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return &UpstreamError{Status: status, Body: string(raw)}
	}
	return nil
}

func (c *Client) findUserID(ctx context.Context, username string) (string, error) {
	path := "/users?username=" + url.QueryEscape(username) + "&exact=true"
	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &UpstreamError{Status: status, Body: string(body)}
	}
	var users []keycloakUser
	if err := json.Unmarshal(body, &users); err != nil {
		return "", fmt.Errorf("decode created user: %w", err)
	}
	if len(users) == 0 || users[0].ID == "" {
		return "", fmt.Errorf("identity service did not return the created user")
	}
	return users[0].ID, nil
}

func (c *Client) realmRoleIndex(ctx context.Context) (map[string]realmRole, error) {
	if time.Now().Before(c.roleIndexCache.expiresAt) && c.roleIndexCache.index != nil {
		return c.roleIndexCache.index, nil
	}
	status, body, err := c.do(ctx, http.MethodGet, "/roles?first=0&max=200", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	var roles []realmRole
	if err := json.Unmarshal(body, &roles); err != nil {
		return nil, fmt.Errorf("decode roles: %w", err)
	}
	index := make(map[string]realmRole, len(SuiteRoles))
	for _, role := range roles {
		if isSuiteRole(role.Name) {
			index[role.Name] = role
		}
	}
	c.roleIndexCache.index = index
	c.roleIndexCache.expiresAt = time.Now().Add(5 * time.Minute)
	return index, nil
}

func (c *Client) roleMapping(ctx context.Context, method, id string, roles []realmRole) error {
	status, body, err := c.do(ctx, method, "/users/"+url.PathEscape(id)+"/role-mappings/realm", roles)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return &UpstreamError{Status: status, Body: string(body)}
	}
	return nil
}

func (c *Client) suiteRoles(ctx context.Context, id string) ([]string, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id)+"/role-mappings/realm/composite", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	var roles []realmRole
	if err := json.Unmarshal(body, &roles); err != nil {
		return nil, fmt.Errorf("decode role mappings: %w", err)
	}
	out := make([]string, 0, len(SuiteRoles))
	for _, role := range roles {
		if isSuiteRole(role.Name) {
			out = append(out, role.Name)
		}
	}
	return out, nil
}

func (c *Client) suiteRoleMembers(ctx context.Context) (map[string][]string, error) {
	out := make(map[string][]string, len(SuiteRoles))
	for _, role := range SuiteRoles {
		members, err := c.roleMembers(ctx, role)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			out[member.ID] = append(out[member.ID], role)
		}
	}
	return out, nil
}

func (c *Client) roleMembers(ctx context.Context, role string) ([]User, error) {
	out := []User{}
	first := 0
	const max = 200
	for {
		path := "/roles/" + url.PathEscape(role) + "/users?first=" + strconv.Itoa(first) + "&max=" + strconv.Itoa(max)
		status, body, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, &UpstreamError{Status: status, Body: string(body)}
		}
		var members []keycloakUser
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, fmt.Errorf("decode role members: %w", err)
		}
		for i := range members {
			out = append(out, projectUser(&members[i]))
		}
		if len(members) < max {
			return out, nil
		}
		first += max
	}
}

func (c *Client) ResetPassword(ctx context.Context, id, password string, temporary bool) error {
	cred := keycloakCredential{Type: "password", Value: password, Temporary: temporary}
	status, body, err := c.do(ctx, http.MethodPut, "/users/"+url.PathEscape(id)+"/reset-password", cred)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return &UpstreamError{Status: status, Body: string(body)}
	}
	return nil
}

func (c *Client) SetRoles(ctx context.Context, id string, roles []string) error {
	desired := make(map[string]bool, len(roles))
	for _, role := range roles {
		if !isSuiteRole(role) {
			return fmt.Errorf("unknown suite role %q", role)
		}
		// The baseline "user" role is inherited by every account through
		// the realm default role. NS neither grants nor removes it; it is
		// excluded from the direct-mapping reconciliation below.
		if role == BaselineSuiteRole {
			continue
		}
		desired[role] = true
	}

	current, err := c.suiteRoles(ctx, id)
	if err != nil {
		return err
	}
	roleIndex, err := c.realmRoleIndex(ctx)
	if err != nil {
		return err
	}

	var add, remove []realmRole
	for _, name := range current {
		if name == BaselineSuiteRole {
			continue
		}
		if !desired[name] {
			remove = append(remove, roleIndex[name])
		}
	}
	for name := range desired {
		if !containsString(current, name) {
			role, ok := roleIndex[name]
			if !ok {
				return fmt.Errorf("suite role %q is not defined in the realm", name)
			}
			add = append(add, role)
		}
	}
	if len(add) > 0 {
		if err := c.roleMapping(ctx, http.MethodPost, id, add); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if err := c.roleMapping(ctx, http.MethodDelete, id, remove); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) EnabledAdminCount(ctx context.Context) (int, error) {
	members, err := c.roleMembers(ctx, "admin")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, member := range members {
		if member.Enabled {
			count++
		}
	}
	return count, nil
}

// UpstreamError represents an error from the identity service.
type UpstreamError struct {
	Status int
	Body   string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("identity service error: status=%d body=%s", e.Status, e.Body)
}

// isSuiteRole checks if a role is one of the known suite roles.
func isSuiteRole(role string) bool {
	for _, r := range SuiteRoles {
		if r == role {
			return true
		}
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
