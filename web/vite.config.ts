// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development the Vite dev server serves the SPA and proxies the API, SSO
// handshake and product gateway routes to the Go backend on :8081, so the
// browser stays same-origin and the HttpOnly session cookie works.
export default defineConfig({
  plugins: [react()],
  server: {
    // 5174 because 5173 is used by the NereidLink dev server on this machine.
    port: 5174,
    strictPort: true,
    host: true,
    proxy: {
      '/api': { target: 'http://localhost:8081', changeOrigin: true },
      '/auth': { target: 'http://localhost:8081', changeOrigin: true },
      '/p': { target: 'http://localhost:8081', changeOrigin: true },
      '/healthz': { target: 'http://localhost:8081', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        // Split the heavy, rarely-changing vendor bundles so the app code
        // stays a small, cacheable chunk.
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          antd: ['antd', '@ant-design/icons'],
          markdown: ['react-markdown', 'remark-gfm'],
          i18n: ['i18next', 'react-i18next', 'i18next-browser-languagedetector'],
        },
      },
    },
  },
})
