# Copyright (C) 2026 YIQISOFT
# SPDX-License-Identifier: Apache-2.0

SHELL := /bin/bash
.DEFAULT_GOAL := help

# Optional local build acceleration: copy local.mk.example to local.mk
# (git-ignored) and set regional proxy/mirror endpoints there. A missing file
# means a pristine direct-internet build, as used by CI.
-include local.mk

.PHONY: help build build-local clean test test-cover vet lint fmt verify \
	web-install web-dev web-build web-typecheck web-check image \
	helm-dep helm-lint helm-template helm-package compose-config dev

BINARY := nereidsphere
# No VERSION file: the git tag is the single version source. On an exact v*
# tag checkout use its bare SemVer (v0.0.1 -> 0.0.1); any other checkout
# builds the rolling dev channel 0.0.0-dev. CI may inject VERSION.
VERSION ?= $(shell ref=$$(git describe --tags --exact-match 2>/dev/null) && echo "$${ref#v}" || echo 0.0.0-dev)
CHART_NAME := nereidsphere
CHART_DIR := deploy/helm
RELEASE := nereidsphere
NAMESPACE := nereidsphere
VALUES ?=
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.GitSHA=$(GIT_SHA)"
GOOS ?= linux
GOARCH ?= amd64
DOCKER := $(shell docker info >/dev/null 2>&1 && echo docker || echo "sudo docker")

# Automated GHCR publication belongs exclusively to GitHub Actions. This
# Makefile only builds/packages and contains no push target. Registry copies
# are an operator's own manual tag/push decision.
REGISTRY ?= ghcr.io/nereidai
IMAGE_NAME ?= nereidsphere
TAG ?= $(VERSION)
IMAGE ?= $(REGISTRY)/$(IMAGE_NAME):$(TAG)
# Release three-tag scheme: VERSION plus UTC-timestamp and short-SHA
# traceability tags; no latest tag anywhere.
BUILD_TS := $(shell date -u +%Y%m%d-%H%M)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORMS ?= linux/amd64
comma := ,
LOCAL_PLATFORM = $(firstword $(subst $(comma), ,$(PLATFORMS)))

# Build-network endpoints are empty by default and injected through the
# environment; empty values keep pristine upstream sources.
export GOPROXY ?=
ALPINE_MIRROR ?=
NPM_REGISTRY ?=
NETWORK_BUILD_ARGS := --build-arg GOPROXY=$(GOPROXY) \
	--build-arg ALPINE_MIRROR=$(ALPINE_MIRROR) \
	--build-arg NPM_REGISTRY=$(NPM_REGISTRY)

help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Available targets:\n"} \
	/^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: web-build ## Build the linux binary with embedded web assets
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/sphere

build-local: web-build ## Build a binary for the current platform
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/sphere

clean: ## Remove local build and packaging output
	rm -rf bin/ dist/ coverage.out coverage.html
	@if [ -d web/dist ]; then \
		find web/dist -mindepth 1 -maxdepth 1 ! -name .gitkeep -exec rm -rf {} +; \
	fi

test: ## Run all Go tests
	go test -v ./...

test-cover: ## Generate the Go coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint when installed
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout=120s; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

fmt: ## Format Go and frontend sources
	go fmt ./...
	cd web && npx prettier --write "src/**/*.{ts,tsx,css,md}" 2>/dev/null || true

verify: ## Run the complete CI and pre-commit verification gate
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-clean:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) web-install
	$(MAKE) web-check
	$(MAKE) build
	$(MAKE) helm-lint
	$(MAKE) helm-template
	$(MAKE) compose-config

web-install: ## Install locked frontend dependencies
	cd web && npm ci

web-dev: ## Start the frontend development server
	cd web && npm run dev

web-build: ## Build the production frontend
	cd web && npm run build

web-typecheck: ## Run the TypeScript type check
	cd web && npm run typecheck

web-check: ## Check frontend localization, help docs and types
	cd web && npm run check && npm run typecheck

image: ## Build the local image with the three release tags; never pushes
	$(DOCKER) build --platform $(LOCAL_PLATFORM) --provenance=false --sbom=false \
		$(NETWORK_BUILD_ARGS) --build-arg VERSION=$(VERSION) \
		--build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile \
		-t $(REGISTRY)/$(IMAGE_NAME):$(TAG) \
		-t $(REGISTRY)/$(IMAGE_NAME):$(TAG)-$(BUILD_TS) \
		-t $(REGISTRY)/$(IMAGE_NAME):$(TAG)-$(GIT_SHA) \
		--load .

helm-dep: ## Resolve Helm chart dependencies
	helm dependency update $(CHART_DIR)

helm-lint: ## Lint the Helm chart
	helm lint $(CHART_DIR)

# The chart deliberately requires a real Valkey Secret for installation. The
# render-only placeholder exercises the complete default HA templates; an
# operator-supplied VALUES file replaces it and must provide real settings.
helm-template: ## Render and validate the complete Helm chart
	helm template $(RELEASE) $(CHART_DIR) \
		$(if $(VALUES),--values "$(VALUES)",--set valkey.auth.password=ci-render-only-placeholder) >/dev/null

compose-config: ## Validate the OIDC Compose environment with placeholder values
	docker compose --env-file .env.example -f docker-compose.yaml config --quiet

helm-package: helm-lint ## Package the chart for offline redistribution
	mkdir -p dist/charts
	helm package $(CHART_DIR) \
		--version $(VERSION) --app-version $(VERSION) \
		-d dist/charts
	@echo "packaged dist/charts/$(CHART_NAME)-$(VERSION).tgz"

# Chart publication to a ChartMuseum is an operator action outside the
# Makefile (no push targets). After `make helm-package`, upload manually with
# the explicit site URL:
#   curl -f --data-binary @dist/charts/<chart>-<version>.tgz \
#        <chartmuseum-url>/api/charts

dev: ## Run NereidSphere with the local development config
	go run ./cmd/sphere serve -c configs/dev.yaml
