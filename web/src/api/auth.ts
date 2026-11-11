// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Authentication and product-catalogue API calls used by the portal.
 *
 * The browser only ever talks to NereidSphere. In oidc mode login is a
 * browser redirect to /auth/login (authorization-code + PKCE handled by the
 * backend); there is no password API. In local emergency mode credentials
 * are posted to /api/v1/auth/login. All calls are same-origin and rely on the
 * HttpOnly session cookie, so no upstream token is ever exposed to JS.
 */

import { get, post } from './client'

/** Identity is the authenticated user returned by GET /api/v1/me. */
export interface Identity {
  sub: string
  email: string
  name: string
  preferred_username: string
  roles: string[]
}

/** Product is one integrated suite product from GET /api/v1/products. */
export interface Product {
  id: string
  name: string
  description: string
  url: string
  mode: 'ns' | 'oidc'
  launch_url: string
  /** Assigned is false when the session has no tenant/role group for it. */
  assigned: boolean
}

/** AuthMethod describes which login experience the backend expects. */
export interface AuthMethod {
  mode: 'local' | 'oidc'
}

/** LoginResult is returned by the local-mode POST /api/v1/auth/login. */
export interface LoginResult {
  identity: Identity
  redirect: string
}

/** fetchIdentity returns the current user, or null when unauthenticated. */
export async function fetchIdentity(): Promise<Identity | null> {
  try {
    return await get<Identity>('/api/v1/me')
  } catch {
    return null
  }
}

/** fetchAuthMethod returns the configured login backend. */
export async function fetchAuthMethod(): Promise<AuthMethod> {
  return get<AuthMethod>('/api/v1/auth/method')
}

/** startSSO builds the browser URL that starts authorization-code + PKCE. */
export function ssoLoginURL(redirect?: string): string {
  const params = new URLSearchParams()
  if (redirect) params.set('redirect', redirect)
  const query = params.toString()
  return `/auth/login${query ? `?${query}` : ''}`
}

/**
 * login submits credentials only in local emergency mode. The oidc backend
 * answers 404; the UI must use ssoLoginURL there.
 */
export async function login(
  username: string,
  password: string,
  redirect?: string,
): Promise<LoginResult> {
  return post<LoginResult>('/api/v1/auth/login', { username, password, redirect })
}

/** logout ends the NS session and revokes the upstream refresh token. */
export async function logout(): Promise<void> {
  await post('/api/v1/auth/logout')
}

/** listProducts returns the suite product catalogue. */
export async function listProducts(): Promise<Product[]> {
  return get<Product[]>('/api/v1/products')
}
