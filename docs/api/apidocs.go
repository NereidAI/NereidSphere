// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package apidocs embeds the OpenAPI contract (openapi.yaml) and a
// self-contained Swagger UI. Both the spec and the vendored swagger-ui-dist
// assets ship inside the binary, so /api/docs works in air-gapped
// on-premises clusters without any external CDN.
//
// openapi.yaml is the single source of truth for the NereidSphere HTTP
// contract; this package only embeds that file and never mutates it.
package apidocs

import (
	"embed"
	"io/fs"
)

// SpecName is the embedded OpenAPI document filename.
const SpecName = "openapi.yaml"

//go:embed openapi.yaml
var specFS embed.FS

//go:embed swaggerui
var swaggerUIFS embed.FS

// Spec returns the raw OpenAPI document bytes. An error is only possible if
// the embedded tree layout changes, which is a build-time defect.
func Spec() ([]byte, error) {
	return specFS.ReadFile(SpecName)
}

// AssetFS returns the Swagger UI static tree rooted at the swaggerui
// directory so that file-server paths match the request URL suffix.
func AssetFS() (fs.FS, error) {
	return fs.Sub(swaggerUIFS, "swaggerui")
}
