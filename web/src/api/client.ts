// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Thin same-origin axios client for the NereidSphere portal.
 *
 * Authentication rides the encrypted HttpOnly session cookie, so no token is
 * ever stored in JavaScript; withCredentials lets the browser send it. Error
 * responses use the family-wide {"error":{"code","message","trace_id"}}
 * envelope, which is normalized here into a single message for the UI.
 *
 * State-changing requests carry the double-submit CSRF token: the server sets
 * a JavaScript-readable ns_csrf cookie on first GET and the SPA echoes it in
 * X-Csrf-Token. A cross-site attacker can neither read the cookie nor forge
 * the matching header.
 */

import axios, { AxiosError } from 'axios'

/** ApiError is the normalized error surfaced to callers. */
export interface ApiError {
  status: number
  code: string
  message: string
  traceId?: string
}

/** readCookie returns one cookie value, or ''. */
function readCookie(name: string): string {
  const prefix = `${name}=`
  const entry = document.cookie
    .split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
  return entry ? decodeURIComponent(entry.slice(prefix.length)) : ''
}

/** Client is the shared same-origin HTTP client. */
export const client = axios.create({
  baseURL: '/',
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((config) => {
  const method = (config.method ?? 'get').toLowerCase()
  if (method !== 'get' && method !== 'head' && method !== 'options') {
    const token = readCookie('ns_csrf')
    if (token) {
      config.headers.set('X-Csrf-Token', token)
    }
  }
  return config
})

/**
 * extractError normalizes the standard error envelope into an ApiError.
 * Falls back to an axios/network message when no envelope is present.
 */
export function extractError(err: unknown): ApiError {
  const ax = err as AxiosError<{ error?: { code?: string; message?: string; trace_id?: string } }>
  const envelope = ax.response?.data?.error
  return {
    status: ax.response?.status ?? 0,
    code: envelope?.code ?? 'NETWORK',
    message: envelope?.message ?? ax.message ?? 'network error',
    traceId: envelope?.trace_id,
  }
}

/** get issues a same-origin GET request. */
export async function get<T>(url: string): Promise<T> {
  const res = await client.get<T>(url)
  return res.data
}

/** post issues a same-origin POST request. */
export async function post<T>(url: string, body?: unknown): Promise<T> {
  const res = await client.post<T>(url, body)
  return res.data
}

/** put issues a same-origin PUT request. */
export async function put<T>(url: string, body?: unknown): Promise<T> {
  const res = await client.put<T>(url, body)
  return res.data
}

/** del issues a same-origin DELETE request. */
export async function del<T>(url: string): Promise<T> {
  const res = await client.delete<T>(url)
  return res.data
}
