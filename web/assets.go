// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package webassets embeds the built portal single-page application so the
// production binary is self-contained. The Makefile builds web/dist before
// compiling (see the build target); dist/.gitkeep keeps the directory and the
// fresh-checkout embed valid before the first frontend build. The HTTP handler
// rejects dotfiles, so the placeholder is never browser-accessible.
package webassets

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded SPA rooted at "dist". A placeholder-only or
// partial frontend build fails validation instead of allowing the HTTP layer
// to expose a directory listing or serve an HTML shell whose assets are
// missing.
func DistFS() (fs.FS, error) {
	assets, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	if err := ValidateSPA(assets); err != nil {
		return nil, fmt.Errorf("validate embedded SPA: %w", err)
	}
	return assets, nil
}

var localAssetReference = regexp.MustCompile(`(?:src|href)=["'](/[^"']+)["']`)

// ValidateSPA verifies that the embedded SPA has a regular index.html and
// that every local file referenced by that document exists as a regular file.
// External https references are intentionally ignored.
func ValidateSPA(assets fs.FS) error {
	index, err := assets.Open("index.html")
	if err != nil {
		return fmt.Errorf("open index.html: %w", err)
	}
	defer func() { _ = index.Close() }()

	info, err := index.Stat()
	if err != nil {
		return fmt.Errorf("stat index.html: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("index.html is a directory")
	}

	document, err := io.ReadAll(index)
	if err != nil {
		return fmt.Errorf("read index.html: %w", err)
	}

	for _, match := range localAssetReference.FindAllSubmatch(document, -1) {
		name := strings.TrimPrefix(string(match[1]), "/")
		name = strings.Split(name, "?")[0]
		name = strings.Split(name, "#")[0]
		if name == "" || name == "index.html" {
			continue
		}

		file, err := assets.Open(name)
		if err != nil {
			return fmt.Errorf("open referenced SPA asset %q: %w", name, err)
		}
		assetInfo, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil {
			return fmt.Errorf("stat referenced SPA asset %q: %w", name, statErr)
		}
		if assetInfo.IsDir() {
			return fmt.Errorf("referenced SPA asset %q is a directory", name)
		}
	}
	return nil
}
