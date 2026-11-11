// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Route table for the NereidSphere portal.
 *
 * /login renders the SSO entry (authorization-code + PKCE starts at the
 * backend /auth/login) or the emergency local form. /help is public. The
 * /admin area hosts identity user management and connection status.
 */

import { Routes, Route } from 'react-router-dom'
import PortalPage from './pages/PortalPage'
import LoginPage from './pages/LoginPage'
import HelpCenterPage from './pages/HelpCenterPage'
import NotFoundPage from './pages/NotFoundPage'
import AdminUsersPage from './pages/AdminUsersPage'
import ConnectionStatusPage from './pages/ConnectionStatusPage'
import AdminLayout from './components/AdminLayout'
import HelpDrawer from './help/HelpDrawer'

/** App declares the portal's client-side routes. */
export default function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/help" element={<HelpCenterPage />} />
        <Route path="/help/glossary/:termId" element={<HelpCenterPage />} />
        <Route path="/help/:docId" element={<HelpCenterPage />} />
        <Route
          path="/admin/users"
          element={
            <AdminLayout>
              <AdminUsersPage />
            </AdminLayout>
          }
        />
        <Route
          path="/admin/status"
          element={
            <AdminLayout>
              <ConnectionStatusPage />
            </AdminLayout>
          }
        />
        <Route path="/" element={<PortalPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
      <HelpDrawer />
    </>
  )
}
