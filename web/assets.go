// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package webassets embeds the built portal single-page application so the
// production binary is self-contained. The Makefile builds web/dist before
// compiling (see the build target); dist/.gitkeep keeps the embed valid
// before the first frontend build.
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded SPA rooted at "dist".
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
