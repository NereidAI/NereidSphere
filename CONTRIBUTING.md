<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Contributing to NereidSphere

Thank you for contributing. NereidSphere is the suite's thin portal, identity
federation, and gateway layer; product-owned domain logic belongs in the
relevant product repository rather than here.

## Development prerequisites

Install Go 1.27.3, Node.js 24 or newer with npm, Make, Docker with the Compose
plugin, and Helm. Install golangci-lint as well if you want the optional lint
step to run locally; the Makefile reports and skips it when it is unavailable.

## Build and verification commands

Run the relevant checks before opening a pull request:

```bash
make verify
make test
make vet
make image

cd web
npm run check
npm run typecheck
npm run build
cd ..

make helm-lint
make helm-template
make compose-config
```

`make verify` is the complete local CI gate. In order, it checks gofmt output,
runs `go vet`, runs golangci-lint when installed, runs all Go tests, installs
locked web dependencies with `npm ci`, checks i18n/help structure and
TypeScript types, builds the frontend and embedded Go binary, lints and
renders the Helm chart, and validates the OIDC Compose configuration using
non-secret placeholder values.

`make test` runs `go test -v ./...`; `make vet` runs `go vet ./...`. `make
image` builds and tags images locally and never pushes them. In the web
directory, `npm run check` runs both the i18n key/call-site assertions and the
bilingual help-document checks, `npm run typecheck` runs `tsc --noEmit`, and
`npm run build` performs the type check before creating production assets.
`make helm-lint` and `make helm-template` validate the chart without installing
it into a cluster. `make compose-config` validates the Compose model without
starting containers or reading a developer's private `.env`.

## Developer Certificate of Origin

Every commit must include a `Signed-off-by` trailer matching its author. Use:

```bash
git commit -s
```

The sign-off certifies the terms of the [Developer Certificate of Origin
1.1](https://developercertificate.org/). Contributions are accepted under the
repository's Apache License 2.0. The DCO workflow checks every non-merge commit
in a pull request.

## Copyright and SPDX headers

New hand-written source, configuration, template, script, workflow, and
Markdown files must start with the YIQISOFT copyright and
`SPDX-License-Identifier: Apache-2.0` header using the file format's comment
syntax. Preserve existing headers when editing a file.

The current exceptions are:

- `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES`, which are legal texts;
- JSON files, `go.mod`, `go.sum`, and `web/package-lock.json`;
- the zero-byte `config/.gitkeep` and `web/dist/.gitkeep` placeholders;
- all vendored files under `docs/api/swaggerui/`; and
- the binary `docs/images/nereidsphere.png`.

Do not add a `VERSION` file or modify the standard `LICENSE` text.

## Language, localization, and help documents

Runtime code, comments, configuration, API messages, and ordinary documentation
must be written in English. Chinese is retained only where it is part of the
product's delivered localization and bilingual help functionality.

When changing user-facing text, keep the English and Chinese locale key sets
and all static translation call sites synchronized. Help pages and glossary
terms must be maintained in both `web/src/docs/en/` and `web/src/docs/zh/`,
registered correctly, and checked with `npm run check`. Do not translate or
remove the Chinese language name merely to satisfy the English-only source
rule.

## Vendored assets and API changes

Do not edit files under `docs/api/swaggerui/` directly. A deliberate upstream
vendoring update must preserve the upstream license and attribution and record
the exact version and provenance in that directory's README.

For HTTP contract changes, update `docs/api/openapi.yaml`, the relevant
SSO/OIDC/gateway documentation, and `FEATURES.md` in the same change. Do not
add product domain logic to the thin suite layer. Report security
vulnerabilities privately through `SECURITY.md` rather than in a public issue.
