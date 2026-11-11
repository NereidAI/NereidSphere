// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// UserGroup is a group membership projection returned by the identity service.
// It carries no children: nested groups are never expanded by the engine's
// group listing, so callers resolve paths one level at a time.
type UserGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// NIAdminClient talks to the identity service Admin REST API for group
// (product/tenant/role) membership management. It uses the same
// nereidsphere-admin service account as Client but is a distinct type so
// group operations are only available when the admin client is configured.
type NIAdminClient struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenCache   struct {
		token     string
		expiresAt time.Time
	}
}

// NewNIAdminClient creates an NI Admin REST client for group management.
// baseURL is the OIDC issuer URL (e.g. https://sso.example/auth/realms/nereidai).
func NewNIAdminClient(baseURL, clientID, clientSecret string) *NIAdminClient {
	return &NIAdminClient{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// ensureToken fetches a valid access token via client_credentials.
func (c *NIAdminClient) ensureToken(ctx context.Context) error {
	if time.Now().Before(c.tokenCache.expiresAt.Add(-30 * time.Second)) {
		return nil // still valid
	}
	tokenURL := c.baseURL + "/protocol/openid-connect/token"
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
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

// do executes a request against the realm Admin REST API.
func (c *NIAdminClient) do(ctx context.Context, method, path string, body any) (int, []byte, http.Header, error) {
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
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(reqBody))
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

// GetUserGroups returns every group the user is a member of.
func (c *NIAdminClient) GetUserGroups(ctx context.Context, userID string) ([]UserGroup, error) {
	var groups []UserGroup
	status, body, _, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(userID)+"/groups", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	if len(body) == 0 {
		return []UserGroup{}, nil
	}
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decode user groups: %w", err)
	}
	return groups, nil
}

// AddUserToGroup adds the user to the group (idempotent in Keycloak).
func (c *NIAdminClient) AddUserToGroup(ctx context.Context, userID, groupID string) error {
	status, body, _, err := c.do(ctx, http.MethodPut, "/users/"+url.PathEscape(userID)+"/groups/"+url.PathEscape(groupID), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return &UpstreamError{Status: status, Body: string(body)}
	}
	return nil
}

// DeleteUserFromGroup removes the user from the group.
func (c *NIAdminClient) DeleteUserFromGroup(ctx context.Context, userID, groupID string) error {
	status, body, _, err := c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(userID)+"/groups/"+url.PathEscape(groupID), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return &UpstreamError{Status: status, Body: string(body)}
	}
	return nil
}

// FindGroupByPath returns the ID of the group with the exact path.
func (c *NIAdminClient) FindGroupByPath(ctx context.Context, path string) (string, error) {
	id, found, err := c.resolvePath(ctx, path)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("group not found: %s", path)
	}
	return id, nil
}

// resolvePath walks path one segment at a time and returns the ID of the last
// segment found, stopping at the first missing one.
func (c *NIAdminClient) resolvePath(ctx context.Context, path string) (string, bool, error) {
	parts := splitGroupPath(path)
	if len(parts) == 0 {
		return "", false, fmt.Errorf("invalid group path: %q", path)
	}
	parentID := ""
	for _, part := range parts {
		id, ok, err := c.findChild(ctx, parentID, part)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		parentID = id
	}
	return parentID, true, nil
}

// groupEnsureMu serializes hierarchy creation within this process. NS runs
// multiple replicas and the admin UI can issue duplicate requests; Keycloak's
// group creation is check-then-insert, so concurrent requests for the same
// path race (one gets 409, or 500 with a sibling_names constraint error).
// Cross-replica races are handled by conflict adoption below.
var (
	groupEnsureMu sync.Mutex

	conflictAdoptDelay    = 200 * time.Millisecond
	conflictAdoptAttempts = 5
)

// EnsureGroupExists creates the group hierarchy along path if missing and
// returns the leaf group ID. Path must be "/nereid{product}/{tenant}/{role}".
// It is safe for concurrent callers and for multiple NS replicas: when
// another writer created the same segment first, the existing group is
// adopted instead of failing.
func (c *NIAdminClient) EnsureGroupExists(ctx context.Context, path string) (string, error) {
	groupEnsureMu.Lock()
	defer groupEnsureMu.Unlock()

	parts := splitGroupPath(path)
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid group path: %q", path)
	}
	var parentID string
	for i, part := range parts {
		current := "/" + strings.Join(parts[:i+1], "/")
		id, ok, err := c.findChild(ctx, parentID, part)
		if err != nil {
			return "", err
		}
		if ok {
			parentID = id
			continue
		}
		// Missing: create under the current parent (or the realm root).
		newID, err := c.createGroupAdopting(ctx, parentID, part, current)
		if err != nil {
			return "", err
		}
		parentID = newID
	}
	if parentID == "" {
		return "", fmt.Errorf("group not found: %s", path)
	}
	return parentID, nil
}

// createGroupAdopting creates one hierarchy segment under parentID (an empty
// parentID means the realm root). Keycloak answers 409 for a plain duplicate
// and 500 (PostgreSQL sibling_names unique violation) when two
// check-then-create transactions race past each other. Both mean "the group
// now exists" — re-read it and adopt the winner's ID.
func (c *NIAdminClient) createGroupAdopting(ctx context.Context, parentID, name, current string) (string, error) {
	createPath := "/groups"
	if parentID != "" {
		createPath = "/groups/" + url.PathEscape(parentID) + "/children"
	}
	status, body, headers, err := c.do(ctx, http.MethodPost, createPath, map[string]string{"name": name})
	if err != nil {
		return "", fmt.Errorf("create group %s: %w", current, err)
	}
	if status == http.StatusCreated || status == http.StatusOK {
		// Prefer the re-read children list: the Location id can be opaque or
		// contain path-like characters; fall back to it only when the list is
		// stale.
		if id, ok, _ := c.findChild(ctx, parentID, name); ok {
			return id, nil
		}
		if id := groupIDFromLocation(headers.Get("Location")); id != "" {
			return id, nil
		}
		return "", fmt.Errorf("created group %s not found", current)
	}
	if !isSiblingCreateConflict(status, body) {
		return "", &UpstreamError{Status: status, Body: string(body)}
	}

	var lastErr error
	for attempt := 0; attempt < conflictAdoptAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(conflictAdoptDelay):
		}
		id, ok, err := c.findChild(ctx, parentID, name)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return id, nil
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("create group %s: sibling conflict, adopt failed: %w", current, lastErr)
	}
	return "", fmt.Errorf("create group %s: sibling conflict but group never appeared: %s",
		current, strings.TrimSpace(string(body)))
}

// groupChildren lists the direct children of parentID; an empty parentID lists
// the realm's top-level groups.
//
// Keycloak 26 does not expand nested subGroups in GET /groups (it reports
// subGroupCount only), so a flat tree read can never see groups below the first
// level. Reading one level at a time through the children endpoint is the only
// reliable way to resolve a nested path: otherwise every lookup misses, the
// caller re-creates a group that already exists, and Keycloak answers 500
// sibling_names.
func (c *NIAdminClient) groupChildren(ctx context.Context, parentID string) ([]UserGroup, error) {
	path := "/groups?briefRepresentation=false&max=1000"
	if parentID != "" {
		path = "/groups/" + url.PathEscape(parentID) + "/children?max=1000"
	}
	status, body, _, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &UpstreamError{Status: status, Body: string(body)}
	}
	if len(body) == 0 {
		return nil, nil
	}
	var groups []UserGroup
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decode group children: %w", err)
	}
	return groups, nil
}

// findChild returns the ID of the direct child of parentID named name.
func (c *NIAdminClient) findChild(ctx context.Context, parentID, name string) (string, bool, error) {
	kids, err := c.groupChildren(ctx, parentID)
	if err != nil {
		return "", false, err
	}
	for _, g := range kids {
		if g.Name == name && g.ID != "" {
			return g.ID, true, nil
		}
	}
	return "", false, nil
}

// isSiblingCreateConflict recognizes Keycloak's duplicate-group responses:
// clean 409 and the 500 produced by a racing insert (SQLState 23505,
// constraint sibling_names).
func isSiblingCreateConflict(status int, body []byte) bool {
	if status == http.StatusConflict {
		return true
	}
	if status < 500 {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "sibling_names") ||
		strings.Contains(lower, "duplicate key") ||
		strings.Contains(lower, "already exists")
}

// splitGroupPath splits "/nereidlink/tenant/role" into
// ["nereidlink", "tenant", "role"].
func splitGroupPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// groupIDFromLocation extracts the group ID from a Create Group Location
// header like ".../admin/realms/nereidai/groups/<id>".
func groupIDFromLocation(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if idx := strings.LastIndex(location, "/"); idx >= 0 && idx+1 < len(location) {
		return location[idx+1:]
	}
	return ""
}
