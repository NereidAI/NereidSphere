// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Authentication bootstrap hook. On first mount it resolves the current
 * identity from the server (the session itself lives in an HttpOnly cookie).
 * The hook exposes the loading state so routes can guard before rendering,
 * and never stores tokens in the browser.
 */

import { useEffect, useState } from 'react'
import { fetchIdentity, Identity } from '../api/auth'

/** useAuth resolves and exposes the current session identity. */
export function useAuth() {
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    fetchIdentity()
      .then((me) => {
        if (active) setIdentity(me)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  return { identity, loading, isAuthenticated: identity !== null }
}
