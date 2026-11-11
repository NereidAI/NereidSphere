// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

import { del, get, post, put } from './client'

export interface AdminUser {
  id: string
  username: string
  email?: string
  first_name?: string
  last_name?: string
  enabled: boolean
  email_verified: boolean
  federated: boolean
  federation_provider?: string
  required_actions: string[]
  roles: string[]
  created_at?: number
}

export interface AdminUserPage {
  users: AdminUser[]
  total: number
  first: number
  max: number
}

export interface CreateUserPayload {
  username: string
  email?: string
  first_name?: string
  last_name?: string
  password: string
  temporary: boolean
  roles: string[]
  product_roles?: ProductRoleAssignments
}

export interface UpdateUserPayload {
  email: string
  first_name: string
  last_name: string
}

export interface ResetPasswordPayload {
  password: string
  temporary: boolean
}

export interface ConnectionStatus {
  mode: 'local' | 'oidc'
  issuer?: string
  issuer_ready: boolean
  admin_api: boolean
  products: Array<{ id: string; name: string; mode: 'ns' | 'oidc' }>
}

export async function adminStatus(): Promise<ConnectionStatus> {
  return get<ConnectionStatus>('/api/v1/admin/status')
}

export async function listAdminUsers(search = '', first = 0, max = 20): Promise<AdminUserPage> {
  const params = new URLSearchParams({ first: String(first), max: String(max) })
  if (search) params.set('search', search)
  return get<AdminUserPage>(`/api/v1/admin/users?${params.toString()}`)
}

export async function createAdminUser(payload: CreateUserPayload): Promise<{ id: string }> {
  return post<{ id: string }>('/api/v1/admin/users', payload)
}

export async function updateAdminUser(id: string, payload: UpdateUserPayload): Promise<void> {
  await put(`/api/v1/admin/users/${encodeURIComponent(id)}`, payload)
}

export async function deleteAdminUser(id: string): Promise<void> {
  await del(`/api/v1/admin/users/${encodeURIComponent(id)}`)
}

export async function setUserEnabled(id: string, enabled: boolean): Promise<void> {
  await post(`/api/v1/admin/users/${encodeURIComponent(id)}/enabled`, { enabled })
}

export async function resetUserPassword(id: string, payload: ResetPasswordPayload): Promise<void> {
  await post(`/api/v1/admin/users/${encodeURIComponent(id)}/reset-password`, payload)
}

export async function setUserRoles(id: string, roles: string[]): Promise<void> {
  await put(`/api/v1/admin/users/${encodeURIComponent(id)}/roles`, { roles })
}

/** ProductRoleAssignment is one tenant+role pair for a product. */
export interface ProductRoleAssignment {
  tenant_id: string
  role: string
}

/** ProductRoleAssignments maps a product id to its tenant+role pairs. */
export type ProductRoleAssignments = Record<string, ProductRoleAssignment[]>

/** ProductCatalogEntry describes one product's selectable roles. */
export interface ProductCatalogEntry {
  id: string
  name: string
  roles: string[]
  tenants_path?: string
}

/** TenantOption is a normalized tenant/org entry for the picker. */
export interface TenantOption {
  id: string
  name: string
}

/** productCatalog lists products that support suite product-role assignment. */
export async function productCatalog(): Promise<ProductCatalogEntry[]> {
  const res = await get<{ products: ProductCatalogEntry[] }>('/api/v1/admin/product-catalog')
  return res.products ?? []
}

/** getProductRoles returns a user's current product/tenant/role assignments. */
export async function getProductRoles(id: string): Promise<ProductRoleAssignments> {
  const res = await get<{ assignments: ProductRoleAssignments }>(
    `/api/v1/admin/users/${encodeURIComponent(id)}/product-roles`,
  )
  return res.assignments ?? {}
}

/** setProductRoles replaces a user's product/tenant/role assignments. */
export async function setProductRoles(id: string, assignments: ProductRoleAssignments): Promise<ProductRoleAssignments> {
  const res = await put<{ assignments: ProductRoleAssignments }>(
    `/api/v1/admin/users/${encodeURIComponent(id)}/product-roles`,
    { assignments },
  )
  return res.assignments ?? {}
}

/**
 * fetchProductTenants loads a product's tenant list through the NS gateway
 * (/p/{product}{tenantsPath}) using the admin's session, so the product keeps
 * ownership of its tenant data. The response shape differs per product, so it
 * is normalized to a flat {id,name} list here.
 */
export async function fetchProductTenants(productId: string, tenantsPath: string): Promise<TenantOption[]> {
  const path = tenantsPath.startsWith('/') ? tenantsPath : `/${tenantsPath}`
  const raw = await get<unknown>(`/p/${encodeURIComponent(productId)}${path}`)
  return normalizeTenants(raw)
}

/** normalizeTenants flattens the common tenant list envelopes products return. */
export function normalizeTenants(raw: unknown): TenantOption[] {
  const items = extractTenantArray(raw)
  const out: TenantOption[] = []
  for (const item of items) {
    const tenant = toTenantOption(item)
    if (tenant && !out.some((entry) => entry.id === tenant.id)) out.push(tenant)
  }
  return out
}

function extractTenantArray(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw
  if (raw && typeof raw === 'object') {
    const record = raw as Record<string, unknown>
    for (const key of ['items', 'tenants', 'orgs', 'organizations', 'data', 'list', 'results', 'rows']) {
      if (Array.isArray(record[key])) return record[key] as unknown[]
    }
    // Fall back to a plain object map keyed by tenant id.
    const entries = Object.entries(record)
    if (entries.length > 0 && entries.every(([, value]) => value && typeof value === 'object' && !Array.isArray(value))) {
      return entries.map(([key, value]) => ({ id: key, ...(value as Record<string, unknown>) }))
    }
  }
  return []
}

function toTenantOption(item: unknown): TenantOption | null {
  if (typeof item === 'string') {
    const value = item.trim()
    return value ? { id: value, name: value } : null
  }
  if (!item || typeof item !== 'object') return null
  const record = item as Record<string, unknown>
  const pick = (...keys: string[]): string => {
    for (const key of keys) {
      const value = record[key]
      if (typeof value === 'string' && value.trim()) return value.trim()
      if (typeof value === 'number') return String(value)
    }
    return ''
  }
  const id = pick('id', 'tenant_id', 'tenantId', 'tenant', 'key', 'org', 'org_id', 'orgId', 'slug', 'name')
  if (!id) return null
  const name = pick('name', 'tenant_name', 'display_name', 'displayName', 'label', 'title', 'id', 'tenant_id', 'org')
  return { id, name: name || id }
}
