# Copyright (C) 2026 YIQISOFT
# SPDX-License-Identifier: Apache-2.0

# syntax=docker/dockerfile:1

# --- build stage: compile frontend then the self-contained Go binary -------
# Pin the builder to the build host architecture and cross-compile with Go's
# native GOOS/GOARCH so the arm64 variant never builds under QEMU (the
# face-service 25-min hang lesson). The SPA is arch-neutral static assets, so
# building it on the build host is safe; only the lightweight runtime stage
# runs foreign-architecture shell instructions.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24 AS builder

WORKDIR /src

# Build tooling. Regional apk/npm mirrors and the Go proxy are injected as
# build args; empty values keep the pristine upstream sources.
ARG ALPINE_MIRROR=""
ARG NPM_REGISTRY=""
RUN if [ -n "$ALPINE_MIRROR" ]; then \
        sed -i "s|dl-cdn.alpinelinux.org|$ALPINE_MIRROR|g" /etc/apk/repositories; \
    fi \
    && apk add --no-cache bash git make nodejs npm \
    && if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi

ARG GOPROXY=""
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=off

# Resolve Go deps first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Resolve frontend deps first as well.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci

# Build the embedded SPA and static binary for each requested target platform.
COPY . .
# BuildKit supplies these per target; defaults here would override arm64 and
# silently produce an amd64 binary inside an arm64 image variant.
ARG TARGETOS
ARG TARGETARCH
# Default is the rolling dev channel; the release workflow injects bare
# SemVer (0.0.1) derived from the v-prefixed git tag. VERSION flows through
# the Makefile into main.Version via -ldflags.
ARG VERSION=0.0.0-dev
ARG GIT_SHA=unknown
RUN make build GOOS=${TARGETOS} GOARCH=${TARGETARCH} VERSION=${VERSION} GIT_SHA=${GIT_SHA}

# --- runtime stage ---------------------------------------------------------
FROM alpine:3.24.4

# Build arguments are scoped to a stage; redeclare the metadata inputs here.
ARG VERSION=0.0.0-dev
ARG GIT_SHA=unknown
ARG BUILD_DATE=""
ARG ALPINE_MIRROR=""

LABEL license='SPDX-License-Identifier: Apache-2.0' \
      copyright='Copyright (c) 2026: YIQISOFT' \
      org.opencontainers.image.title='nereidsphere' \
      org.opencontainers.image.description='Thin suite layer — single portal, SSO, unified API gateway, and account administration' \
      org.opencontainers.image.vendor='YIQISOFT' \
      org.opencontainers.image.licenses='Apache-2.0' \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source='https://github.com/NereidAI/NereidSphere' \
      org.opencontainers.image.url='https://github.com/NereidAI/NereidSphere' \
      org.opencontainers.image.documentation='https://github.com/NereidAI/NereidSphere#readme' \
      org.opencontainers.image.authors='NereidAI (https://github.com/NereidAI)'

RUN if [ -n "$ALPINE_MIRROR" ]; then \
        sed -i "s|dl-cdn.alpinelinux.org|$ALPINE_MIRROR|g" /etc/apk/repositories; \
    fi \
    && apk add --no-cache ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app \
    && mkdir -p /etc/nereidsphere && chown app:app /etc/nereidsphere

WORKDIR /app
COPY LICENSE NOTICE /licenses/
# The binary embeds the SPA, so no separate web assets are shipped.
COPY --from=builder /src/bin/nereidsphere /app/nereidsphere
# Reference config; production mounts its own file over the path below.
COPY --from=builder /src/configs /app/configs

USER app
EXPOSE 8081

ENTRYPOINT ["/app/nereidsphere"]
# Mount the real configuration at /etc/nereidsphere/config.yaml in production.
CMD ["serve", "-c", "/etc/nereidsphere/config.yaml"]
