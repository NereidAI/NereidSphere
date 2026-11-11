// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package apidocs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDocsServesSwaggerUI verifies the shell references the embedded spec.
func TestDocsServesSwaggerUI(t *testing.T) {
	for _, path := range []string{"/api/docs", "/api/docs/"} {
		rec := httptest.NewRecorder()
		Handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/api/docs/openapi.yaml") {
			t.Fatalf("%s: status=%d body missing spec url", path, rec.Code)
		}
	}
}

// TestDocsServesOpenAPISpec verifies the embedded YAML is served.
func TestDocsServesOpenAPISpec(t *testing.T) {
	rec := httptest.NewRecorder()
	AssetHandler(rec, httptest.NewRequest(http.MethodGet, "/api/docs/openapi.yaml", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "NereidSphere API") {
		t.Fatalf("status=%d, spec not served", rec.Code)
	}
}

// TestDocsServesVendoredAssets verifies the Swagger UI bundle is embedded.
func TestDocsServesVendoredAssets(t *testing.T) {
	rec := httptest.NewRecorder()
	AssetHandler(rec, httptest.NewRequest(http.MethodGet, "/api/docs/swagger-ui-bundle.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle status = %d", rec.Code)
	}
}

// TestDocsUnknownAsset404 prevents fallthrough of unknown files.
func TestDocsUnknownAsset404(t *testing.T) {
	rec := httptest.NewRecorder()
	AssetHandler(rec, httptest.NewRequest(http.MethodGet, "/api/docs/nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status = %d, want 404", rec.Code)
	}
}

// TestDocsMethodGuard rejects non-GET/HEAD verbs on the asset subtree.
func TestDocsMethodGuard(t *testing.T) {
	rec := httptest.NewRecorder()
	AssetHandler(rec, httptest.NewRequest(http.MethodPost, "/api/docs/openapi.yaml", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status = %d, want 405", rec.Code)
	}
}
