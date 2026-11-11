// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/httpx"
)

const maxAdminRequestBodyBytes = 1 << 20

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

type Handler struct {
	client  *Client
	niAdmin *NIAdminClient
	guard   *auth.Resolver
	ready   ReadinessChecker
	cfg     *config.Config
}

func NewHandler(cfg *config.Config, guard *auth.Resolver, client *Client, ready ReadinessChecker) *Handler {
	h := &Handler{client: client, guard: guard, ready: ready, cfg: cfg}
	if cfg.Auth.Mode == config.AuthModeOIDC && cfg.OIDC.AdminClientID != "" && cfg.OIDC.AdminClientSecret != "" && cfg.OIDC.Issuer != "" {
		h.niAdmin = NewNIAdminClient(cfg.OIDC.Issuer, cfg.OIDC.AdminClientID, cfg.OIDC.AdminClientSecret)
	}
	return h
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/status", h.status)
	mux.HandleFunc("GET /api/v1/admin/users", h.listUsers)
	mux.HandleFunc("POST /api/v1/admin/users", h.createUser)
	mux.HandleFunc("GET /api/v1/admin/users/{id}", h.getUser)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}", h.updateUser)
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", h.deleteUser)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/enabled", h.setEnabled)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/reset-password", h.resetPassword)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/roles", h.setRoles)
	// Product tenant/role assignments
	mux.HandleFunc("GET /api/v1/admin/users/{id}/product-roles", h.getProductRoles)
	mux.HandleFunc("PUT /api/v1/admin/users/{id}/product-roles", h.putProductRoles)
	// Product role catalog for the console picker
	mux.HandleFunc("GET /api/v1/admin/product-catalog", h.productCatalog)
}

type productStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

type statusResponse struct {
	Mode        string          `json:"mode"`
	Issuer      string          `json:"issuer,omitempty"`
	IssuerReady bool            `json:"issuer_ready"`
	AdminAPI    bool            `json:"admin_api"`
	Products    []productStatus `json:"products"`
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	if _, err := h.guard.Resolve(nil, r); err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	resp := statusResponse{
		Mode:     h.cfg.Auth.Mode,
		AdminAPI: h.client != nil,
		Products: make([]productStatus, 0, len(h.cfg.Products)),
	}
	for _, p := range h.cfg.Products {
		resp.Products = append(resp.Products, productStatus{ID: p.ID, Name: p.Name, Mode: p.Mode})
	}
	if h.cfg.Auth.Mode == config.AuthModeOIDC {
		resp.Issuer = h.cfg.OIDC.Issuer
		if h.ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			resp.IssuerReady = h.ready.Ready(ctx) == nil
			cancel()
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireRole(w, r, "admin", "operator"); !ok {
		return
	}
	q := r.URL.Query()
	first, err := parseNonNegativeInt(q.Get("first"), 0)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "first must be a non-negative integer")
		return
	}
	max := 50
	if raw := q.Get("max"); raw != "" {
		max, err = parseNonNegativeInt(raw, 50)
		if err != nil || max < 1 || max > 200 {
			httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "max must be between 1 and 200")
			return
		}
	}
	search := strings.TrimSpace(q.Get("search"))
	if len(search) > 128 {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "search must be at most 128 characters")
		return
	}
	page, err := h.client.ListUsers(r.Context(), search, first, max)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireRole(w, r, "admin", "operator"); !ok {
		return
	}
	u, err := h.client.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	var req CreateUserRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Roles = normalizeRoles(req.Roles)
	if len(req.Roles) == 0 {
		req.Roles = []string{"user"}
	}
	if err := validateUsername(req.Username); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	if err := validateEmail(req.Email); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	if err := validateName(req.FirstName); err != nil {
		badRequest(w, r, "first name is too long")
		return
	}
	if err := validateName(req.LastName); err != nil {
		badRequest(w, r, "last name is too long")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	if err := validateRoles(req.Roles); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	if len(req.ProductRoles) > 0 {
		if h.niAdmin == nil {
			badRequest(w, r, "product role assignment requires oidc mode with the admin client configured")
			return
		}
		if err := validateProductRoles(req.ProductRoles); err != nil {
			badRequest(w, r, err.Error())
			return
		}
	}
	id, err := h.client.CreateUser(r.Context(), req)
	if err != nil {
		h.audit(r, actor, "user.create", id, req.Username, err)
		writeUpstreamError(w, r, err)
		return
	}
	if len(req.ProductRoles) > 0 {
		if _, _, applyErr := h.applyProductRoles(r.Context(), id, req.ProductRoles); applyErr != nil {
			slog.ErrorContext(r.Context(), "failed to apply product roles on create", "user_id", id, "error", applyErr)
			if cleanupErr := h.client.DeleteUser(context.WithoutCancel(r.Context()), id); cleanupErr != nil {
				slog.ErrorContext(r.Context(), "failed to remove partially created user", "user_id", id, "error", cleanupErr)
			}
			h.audit(r, actor, "user.create", id, req.Username, applyErr)
			httpx.WriteError(w, r, http.StatusBadGateway, "IDENTITY_ERROR", "failed to assign product roles")
			return
		}
	}
	h.audit(r, actor, "user.create", id, req.Username, nil)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	id := r.PathValue("id")
	target, ok := h.loadMutableUser(w, r, id)
	if !ok {
		return
	}
	var req UpdateUserRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		req.Email = &email
		if err := validateEmail(email); err != nil {
			badRequest(w, r, err.Error())
			return
		}
	}
	if req.FirstName != nil {
		name := strings.TrimSpace(*req.FirstName)
		req.FirstName = &name
		if err := validateName(name); err != nil {
			badRequest(w, r, "first name is too long")
			return
		}
	}
	if req.LastName != nil {
		name := strings.TrimSpace(*req.LastName)
		req.LastName = &name
		if err := validateName(name); err != nil {
			badRequest(w, r, "last name is too long")
			return
		}
	}
	err := h.client.UpdateUser(r.Context(), id, req)
	h.auditTarget(r, actor, "user.update", target, err)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	id := r.PathValue("id")
	target, ok := h.loadMutableUser(w, r, id)
	if !ok {
		return
	}
	if !h.protectAdministrator(w, r, actor, target, "user.delete", true) {
		return
	}
	err := h.client.DeleteUser(r.Context(), id)
	h.auditTarget(r, actor, "user.delete", target, err)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type enabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) setEnabled(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	id := r.PathValue("id")
	target, ok := h.loadMutableUser(w, r, id)
	if !ok {
		return
	}
	var req enabledRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	if !req.Enabled && !h.protectAdministrator(w, r, actor, target, "user.disable", true) {
		return
	}
	err := h.client.SetEnabled(r.Context(), id, req.Enabled)
	h.auditTarget(r, actor, "user.set_enabled", target, err)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resetPasswordRequest struct {
	Password  string `json:"password"`
	Temporary bool   `json:"temporary"`
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	id := r.PathValue("id")
	target, ok := h.loadMutableUser(w, r, id)
	if !ok {
		return
	}
	var req resetPasswordRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	if err := validatePassword(req.Password); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	err := h.client.ResetPassword(r.Context(), id, req.Password, req.Temporary)
	h.auditTarget(r, actor, "user.reset_password", target, err)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type rolesRequest struct {
	Roles []string `json:"roles"`
}

func (h *Handler) setRoles(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	id := r.PathValue("id")
	target, ok := h.loadMutableUser(w, r, id)
	if !ok {
		return
	}
	var req rolesRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	req.Roles = normalizeRoles(req.Roles)
	if len(req.Roles) == 0 {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "at least one role is required")
		return
	}
	if err := validateRoles(req.Roles); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	removesAdmin := containsString(target.Roles, "admin") && !containsString(req.Roles, "admin")
	if removesAdmin && !h.protectAdministrator(w, r, actor, target, "user.update_roles", true) {
		return
	}
	err := h.client.SetRoles(r.Context(), id, req.Roles)
	h.auditTarget(r, actor, "user.update_roles", target, err)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requireRole(w http.ResponseWriter, r *http.Request, allowed ...string) (*auth.SessionData, bool) {
	if h.client == nil {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "user administration requires oidc mode")
		return nil, false
	}
	session, err := h.guard.Resolve(w, r)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return nil, false
	}
	for _, role := range allowed {
		for _, have := range session.Identity.Roles {
			if have == role {
				return session, true
			}
		}
	}
	httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "requires one of roles: "+strings.Join(allowed, ","))
	return nil, false
}

func (h *Handler) loadMutableUser(w http.ResponseWriter, r *http.Request, id string) (*User, bool) {
	target, err := h.client.GetUser(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, r, err)
		return nil, false
	}
	if target.Federated {
		httpx.WriteError(w, r, http.StatusConflict, "FEDERATED_READ_ONLY",
			"this account is managed by an external identity provider and is read-only here")
		return nil, false
	}
	return target, true
}

func (h *Handler) protectAdministrator(w http.ResponseWriter, r *http.Request, actor *auth.SessionData, target *User, action string, checkCount bool) bool {
	if actor.Identity.Sub == target.ID {
		httpx.WriteError(w, r, http.StatusConflict, "SELF_ACTION_FORBIDDEN", "you cannot perform this action on your own account")
		h.auditTarget(r, actor, action, target, errors.New("self action forbidden"))
		return false
	}
	if !checkCount || !target.Enabled || !containsString(target.Roles, "admin") {
		return true
	}
	count, err := h.client.EnabledAdminCount(r.Context())
	if err != nil {
		writeUpstreamError(w, r, err)
		return false
	}
	if count <= 1 {
		httpx.WriteError(w, r, http.StatusConflict, "LAST_ADMIN_PROTECTED", "at least one enabled administrator is required")
		h.auditTarget(r, actor, action, target, errors.New("last admin protected"))
		return false
	}
	return true
}

func (h *Handler) audit(r *http.Request, actor *auth.SessionData, action, targetID, targetUsername string, err error) {
	h.auditTarget(r, actor, action, &User{ID: targetID, Username: targetUsername}, err)
}

func (h *Handler) auditTarget(r *http.Request, actor *auth.SessionData, action string, target *User, err error) {
	attrs := []any{
		"action", action,
		"actor_sub", actor.Identity.Sub,
		"actor_username", actor.Identity.Username,
		"target_id", target.ID,
		"remote_addr", r.RemoteAddr,
		"trace_id", httpx.TraceID(r.Context()),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		slog.WarnContext(r.Context(), "admin account operation failed", attrs...)
		return
	}
	slog.InfoContext(r.Context(), "admin account operation", attrs...)
}

// --- Product Roles Management ---

// ProductRoleAssignment represents a tenant+role pair for a product.
type ProductRoleAssignment struct {
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
}

// ProductRolesRequest is the request/response body for product roles.
type ProductRolesRequest struct {
	Assignments map[string][]ProductRoleAssignment `json:"assignments"`
}

// ProductCatalogEntry describes one product's selectable product roles.
type ProductCatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Roles       []string `json:"roles"`
	TenantsPath string   `json:"tenants_path,omitempty"`
}

// ProductCatalogResponse is the response body for the product-role catalog.
type ProductCatalogResponse struct {
	Products []ProductCatalogEntry `json:"products"`
}

// productCatalog lists the products that participate in the suite
// product-role convention, their selectable roles and the product-relative
// tenant list path the console fetches through the gateway.
func (h *Handler) productCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireRole(w, r, "admin", "operator"); !ok {
		return
	}
	if h.niAdmin == nil {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "product roles require oidc mode with admin client configured")
		return
	}
	entries := make([]ProductCatalogEntry, 0, len(h.cfg.Products))
	for _, p := range h.cfg.Products {
		roles, ok := RolesForProduct(p.ID)
		if !ok {
			continue // product has no suite product-role mapping
		}
		roleNames := make([]string, 0, len(roles))
		for _, role := range roles {
			roleNames = append(roleNames, string(role))
		}
		entries = append(entries, ProductCatalogEntry{
			ID:          p.ID,
			Name:        p.Name,
			Roles:       roleNames,
			TenantsPath: p.TenantsPath,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, ProductCatalogResponse{Products: entries})
}

// toProductTenantRoles converts the wire shape into the group utility type.
func toProductTenantRoles(in map[string][]ProductRoleAssignment) ProductTenantRoles {
	out := make(ProductTenantRoles, len(in))
	for product, pairs := range in {
		converted := make([]TenantRolePair, 0, len(pairs))
		for _, p := range pairs {
			converted = append(converted, TenantRolePair{TenantID: p.TenantID, Role: RoleKey(p.Role)})
		}
		out[ProductID(product)] = converted
	}
	return out
}

// fromProductTenantRoles converts group assignments back to the wire shape.
func fromProductTenantRoles(in ProductTenantRoles) map[string][]ProductRoleAssignment {
	out := make(map[string][]ProductRoleAssignment, len(in))
	for product, pairs := range in {
		converted := make([]ProductRoleAssignment, 0, len(pairs))
		for _, p := range pairs {
			converted = append(converted, ProductRoleAssignment{TenantID: p.TenantID, Role: string(p.Role)})
		}
		out[string(product)] = converted
	}
	return out
}

// getProductRoles returns the current product-tenant-role assignments for a user.
func (h *Handler) getProductRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireRole(w, r, "admin", "operator"); !ok {
		return
	}
	if h.niAdmin == nil {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "product roles require oidc mode with admin client configured")
		return
	}
	id := r.PathValue("id")
	// Verify user exists
	_, err := h.client.GetUser(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	// Fetch user's groups from NI
	groups, err := h.niAdmin.GetUserGroups(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	// Parse into product -> tenant+role
	parsed := ParseUserGroups(extractGroupPaths(groups))
	httpx.WriteJSON(w, http.StatusOK, ProductRolesRequest{Assignments: fromProductTenantRoles(parsed)})
}

// allowedProductRoles maps each suite product id to its allowed role keys.
func allowedProductRoles() map[string]map[string]bool {
	roleSet := make(map[string]map[string]bool)
	for _, m := range AllProductRoleMaps() {
		set := make(map[string]bool, len(m.Roles))
		for _, r := range m.Roles {
			set[string(r)] = true
		}
		roleSet[string(m.Product)] = set
	}
	return roleSet
}

// validateProductRoles rejects unknown products, empty tenants and roles that
// are not part of a product's role set.
func validateProductRoles(assignments map[string][]ProductRoleAssignment) error {
	roleSet := allowedProductRoles()
	for product, pairs := range assignments {
		if _, ok := roleSet[product]; !ok {
			return fmt.Errorf("unknown product: %s", product)
		}
		for _, p := range pairs {
			if strings.TrimSpace(p.TenantID) == "" {
				return errors.New("tenant_id is required")
			}
			if !roleSet[product][p.Role] {
				return fmt.Errorf("invalid role %s for product %s", p.Role, product)
			}
		}
	}
	return nil
}

// applyProductRoles reconciles a user's identity-service groups with the
// requested product/tenant/role assignments. It returns the group paths it
// added and removed. Only groups following the suite convention are touched.
func (h *Handler) applyProductRoles(ctx context.Context, userID string, assignments map[string][]ProductRoleAssignment) (added, removed []string, err error) {
	targetGroups := BuildTargetGroups(toProductTenantRoles(assignments))
	current, err := h.niAdmin.GetUserGroups(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	toAdd, toRemove := DiffGroups(suiteGroupPaths(extractGroupPaths(current)), targetGroups)
	for _, g := range toRemove {
		groupID, findErr := h.niAdmin.FindGroupByPath(ctx, g)
		if findErr != nil {
			// Already absent: nothing to remove.
			slog.WarnContext(ctx, "group not found for removal", "group", g, "error", findErr)
			continue
		}
		if delErr := h.niAdmin.DeleteUserFromGroup(ctx, userID, groupID); delErr != nil {
			return added, removed, fmt.Errorf("remove group %s: %w", g, delErr)
		}
		removed = append(removed, g)
	}
	for _, g := range toAdd {
		groupID, ensureErr := h.niAdmin.EnsureGroupExists(ctx, g)
		if ensureErr != nil {
			return added, removed, fmt.Errorf("ensure group %s: %w", g, ensureErr)
		}
		if addErr := h.niAdmin.AddUserToGroup(ctx, userID, groupID); addErr != nil {
			return added, removed, fmt.Errorf("add group %s: %w", g, addErr)
		}
		added = append(added, g)
	}
	return added, removed, nil
}

// putProductRoles updates the product-tenant-role assignments for a user.
// This computes the diff and applies add/remove group operations via NI Admin REST.
func (h *Handler) putProductRoles(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireRole(w, r, "admin")
	if !ok {
		return
	}
	if h.niAdmin == nil {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "product roles require oidc mode with admin client configured")
		return
	}
	id := r.PathValue("id")
	target, err := h.client.GetUser(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	if target.Federated {
		httpx.WriteError(w, r, http.StatusConflict, "FEDERATED_READ_ONLY",
			"this account is managed by an external identity provider and is read-only here")
		return
	}
	var req ProductRolesRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	if err := validateProductRoles(req.Assignments); err != nil {
		badRequest(w, r, err.Error())
		return
	}
	toAdd, toRemove, err := h.applyProductRoles(r.Context(), id, req.Assignments)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to apply product roles", "user_id", id, "error", err)
		httpx.WriteError(w, r, http.StatusBadGateway, "IDENTITY_ERROR", "failed to update product roles")
		return
	}
	// Audit
	attrs := []any{
		"action", "user.update_product_roles",
		"actor_sub", actor.Identity.Sub,
		"actor_username", actor.Identity.Username,
		"target_id", target.ID,
		"target_username", target.Username,
		"added_groups", toAdd,
		"removed_groups", toRemove,
		"trace_id", httpx.TraceID(r.Context()),
	}
	slog.InfoContext(r.Context(), "admin product roles updated", attrs...)
	// Return updated assignments
	updatedGroups, err := h.niAdmin.GetUserGroups(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	parsed := ParseUserGroups(extractGroupPaths(updatedGroups))
	httpx.WriteJSON(w, http.StatusOK, ProductRolesRequest{Assignments: fromProductTenantRoles(parsed)})
}

func extractGroupPaths(groups []UserGroup) []string {
	paths := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Path != "" {
			paths = append(paths, g.Path)
		}
	}
	return paths
}

// suiteGroupPaths keeps only suite-managed group paths (/nereid{product}/...),
// so reconciliation never removes a user from unrelated groups (engine defaults
// or groups owned by another system).
func suiteGroupPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, _, _, err := ParseProductGroup(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return false
	}
	if decoder.More() {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return false
	}
	return true
}

func parseNonNegativeInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return parsed, nil
}

func normalizeRoles(roles []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		out = append(out, role)
	}
	return out
}

func validateUsername(username string) error {
	if username == "" || len(username) > 128 {
		return errors.New("username must be between 1 and 128 characters")
	}
	if strings.ContainsFunc(username, unicode.IsControl) {
		return errors.New("username must not contain control characters")
	}
	return nil
}

func validateEmail(email string) error {
	if email == "" {
		return nil
	}
	if len(email) > 255 {
		return errors.New("email must be at most 255 characters")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("email must be a valid email address")
	}
	return nil
}

func validateName(name string) error {
	if len(name) > 128 {
		return errors.New("name must be at most 128 characters")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(password) > 256 {
		return errors.New("password must be at most 256 characters")
	}
	return nil
}

func validateRoles(roles []string) error {
	for _, role := range roles {
		if !isSuiteRole(role) {
			return errors.New("unknown suite role " + strconv.Quote(role))
		}
	}
	return nil
}

func badRequest(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", message)
}

func writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	var up *UpstreamError
	if errors.As(err, &up) {
		switch up.Status {
		case http.StatusNotFound:
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		case http.StatusConflict:
			httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "username or email already exists")
			return
		case http.StatusForbidden, http.StatusUnauthorized:
			slog.WarnContext(r.Context(), "identity admin API denied the service account",
				"status", up.Status, "trace_id", httpx.TraceID(r.Context()))
			httpx.WriteError(w, r, http.StatusBadGateway, "IDENTITY_FORBIDDEN",
				"identity service rejected the admin service account; check the realm bootstrap role grant")
			return
		default:
			httpx.WriteError(w, r, http.StatusBadGateway, "IDENTITY_ERROR",
				"identity service returned status "+strconv.Itoa(up.Status))
			return
		}
	}
	slog.ErrorContext(r.Context(), "identity admin call failed", "error", err)
	httpx.WriteError(w, r, http.StatusBadGateway, "IDENTITY_UNAVAILABLE", "identity service unavailable")
}
