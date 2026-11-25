// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServeSPAFullBuild(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<!doctype html><title>NereidSphere</title>")},
		"favicon.svg":    &fstest.MapFile{Data: []byte("<svg></svg>")},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log('ok')")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body {}")},
		".gitkeep":       &fstest.MapFile{Data: []byte("placeholder")},
	}

	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantContain string
		notContain  string
		contentType string
	}{
		{name: "root serves index", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantContain: "<!doctype html>", contentType: "text/html"},
		{name: "deep link serves index", method: http.MethodGet, path: "/auth/callback", wantStatus: http.StatusOK, wantContain: "<!doctype html>", contentType: "text/html"},
		{name: "hashed asset is served", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantContain: "console.log", contentType: "text/javascript"},
		{name: "missing asset is a real 404", method: http.MethodGet, path: "/assets/missing.js", wantStatus: http.StatusNotFound},
		{name: "directory does not autoindex", method: http.MethodGet, path: "/assets/", wantStatus: http.StatusNotFound, notContain: ".gitkeep"},
		{name: "dotfile is not web accessible", method: http.MethodGet, path: "/.gitkeep", wantStatus: http.StatusNotFound, notContain: "placeholder"},
		{name: "traversal is rejected", method: http.MethodGet, path: "/../.gitkeep", wantStatus: http.StatusNotFound},
		{name: "post is not allowed", method: http.MethodPost, path: "/auth/callback", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			serveSPA(recorder, httptest.NewRequest(tc.method, tc.path, nil), assets)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body:\n%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			body := recorder.Body.String()
			if tc.wantContain != "" && !strings.Contains(body, tc.wantContain) {
				t.Fatalf("body = %q, want substring %q", body, tc.wantContain)
			}
			if tc.notContain != "" && strings.Contains(body, tc.notContain) {
				t.Fatalf("body unexpectedly contains %q:\n%s", tc.notContain, body)
			}
			if tc.contentType != "" && !strings.Contains(recorder.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("content-type = %q, want %q", recorder.Header().Get("Content-Type"), tc.contentType)
			}
		})
	}
}

func TestServeSPAPlaceholderOnlyEmbed(t *testing.T) {
	assets := fstest.MapFS{
		".gitkeep": &fstest.MapFile{Data: []byte("placeholder")},
	}

	for _, path := range []string{"/", "/auth/callback", "/.gitkeep", "/assets/"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			serveSPA(recorder, httptest.NewRequest(http.MethodGet, path, nil), assets)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body:\n%s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			if strings.Contains(body, ".gitkeep") || strings.Contains(body, "placeholder") {
				t.Fatalf("placeholder embed was exposed:\n%s", body)
			}
		})
	}
}

var _ fs.FS = fstest.MapFS{}
