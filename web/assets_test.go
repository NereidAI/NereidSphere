// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package webassets

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidateSPAAcceptsCompleteBuild(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html>
<script type="module" src="/assets/index-abc.js"></script>
<link rel="stylesheet" href="/assets/index-def.css">
<link rel="icon" href="/favicon.svg">`)},
		"assets/index-abc.js":  &fstest.MapFile{Data: []byte("console.log('ok')")},
		"assets/index-def.css": &fstest.MapFile{Data: []byte("body {}")},
		"favicon.svg":          &fstest.MapFile{Data: []byte("<svg></svg>")},
	}

	if err := ValidateSPA(assets); err != nil {
		t.Fatalf("ValidateSPA returned error: %v", err)
	}
}

func TestValidateSPARejectsPlaceholderOnlyBuild(t *testing.T) {
	assets := fstest.MapFS{
		".gitkeep": &fstest.MapFile{Data: []byte("placeholder")},
	}

	err := ValidateSPA(assets)
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("error = %v, want missing index.html error", err)
	}
}

func TestValidateSPARejectsPartialBuild(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html>
<script type="module" src="/assets/index-abc.js"></script>
<link rel="stylesheet" href="/assets/missing.css">`)},
		"assets/index-abc.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	}

	err := ValidateSPA(assets)
	if err == nil || !strings.Contains(err.Error(), "assets/missing.css") {
		t.Fatalf("error = %v, want missing referenced asset error", err)
	}
}
