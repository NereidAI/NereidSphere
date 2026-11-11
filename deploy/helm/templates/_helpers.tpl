# Copyright (C) 2026 YIQISOFT
# SPDX-License-Identifier: Apache-2.0

{{/*
Expand the name of the chart.
*/}}
{{- define "nereidsphere.name" -}}
{{- default .Chart.Name .Values.nameOverride | lower | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name.
*/}}
{{- define "nereidsphere.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride | lower }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "nereidsphere.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | lower | trunc 63 | trimSuffix "-" }}
{{ include "nereidsphere.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "nereidsphere.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nereidsphere.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Name of the Secret holding config.yaml (rendered or pre-existing).
*/}}
{{- define "nereidsphere.configSecretName" -}}
{{- if .Values.config.existingSecret }}
{{- .Values.config.existingSecret }}
{{- else }}
{{- include "nereidsphere.fullname" . }}-config
{{- end }}
{{- end }}

{{- define "nereidsphere.valkeySecretName" -}}
{{- if .Values.valkey.auth.existingSecret -}}
{{- .Values.valkey.auth.existingSecret -}}
{{- else -}}
{{- include "nereidsphere.fullname" . -}}-valkey
{{- end -}}
{{- end -}}

{{/*
Render the runtime YAML consumed by `sphere serve -c /app/config/config.yaml`.
Kept in one helper so both the generated Secret and local tooling agree on
its shape.
*/}}
{{- define "nereidsphere.config" -}}
{{- $mode := default "oidc" .Values.config.auth.mode -}}
{{- $replayStore := default "memory" .Values.tickets.replayStore -}}
{{- if .Values.valkey.enabled -}}{{- $replayStore = "valkey" -}}{{- end -}}
{{- if and .Values.valkey.enabled (eq .Values.tickets.replayStore "memory") -}}
{{- fail "tickets.replayStore=memory conflicts with built-in valkey.enabled=true" -}}
{{- end -}}
{{- if and .Values.valkey.enabled (not .Values.valkey.auth.password) (not .Values.valkey.auth.existingSecret) -}}
{{- fail "valkey.auth.password or valkey.auth.existingSecret is required when valkey.enabled=true" -}}
{{- end -}}
{{- if and (not .Values.valkey.enabled) (eq $replayStore "valkey") (not .Values.valkey.external.address) (eq (len .Values.valkey.external.sentinelAddresses) 0) -}}
{{- fail "valkey.external.address or valkey.external.sentinelAddresses is required when using external Valkey" -}}
{{- end -}}
{{- if and (not .Values.valkey.enabled) (eq $replayStore "valkey") (not .Values.valkey.external.address) (not .Values.valkey.external.sentinelMasterName) -}}
{{- fail "valkey.external.sentinelMasterName is required for external Sentinel Valkey" -}}
{{- end -}}
{{- if and (gt (int .Values.replicaCount) 1) (eq $replayStore "memory") -}}
{{- fail "multiple replicas require tickets.replayStore=valkey (in-process replay cache cannot deduplicate across pods)" -}}
{{- end -}}
{{- if eq $mode "oidc" }}
{{- if not .Values.config.oidc.issuer }}{{- fail "config.oidc.issuer is required when config.auth.mode=oidc" }}{{ end }}
{{- if not .Values.config.oidc.clientId }}{{- fail "config.oidc.clientId is required when config.auth.mode=oidc" }}{{ end }}
{{- if not .Values.config.oidc.clientSecret }}{{- fail "config.oidc.clientSecret is required when config.auth.mode=oidc" }}{{ end }}
{{- if not .Values.config.oidc.adminClientId }}{{- fail "config.oidc.adminClientId is required when config.auth.mode=oidc" }}{{ end }}
{{- if not .Values.config.oidc.adminClientSecret }}{{- fail "config.oidc.adminClientSecret is required when config.auth.mode=oidc" }}{{ end }}
{{- else if eq $mode "local" }}
{{- if not .Values.config.auth.localUsers }}
{{- fail "config.auth.localUsers must be provided when config.auth.mode=local" }}
{{- end }}
{{- else }}
{{- fail "config.auth.mode must be \"oidc\" or \"local\"" }}
{{- end }}
server:
  listen: ":{{ .Values.service.port }}"
  base_url: {{ required "config.server.baseUrl is required (the external NS origin)" .Values.config.server.baseUrl | quote }}
  env: "prod"

auth:
  mode: {{ $mode | quote }}
{{- with .Values.config.auth.localUsers }}
  local_users:
{{- range . }}
    - username: {{ required "local user needs a username" .username | quote }}
      name: {{ .name | default .username | quote }}
      email: {{ .email | default (printf "%s@local" .username) | quote }}
      password_bcrypt: {{ required "local user needs a bcrypt password hash (passwordBcrypt)" .passwordBcrypt | quote }}
      roles:
{{ toYaml (.roles | default (list "user")) | indent 8 }}
{{- end }}
{{- end }}

oidc:
  issuer: {{ .Values.config.oidc.issuer | default "" | quote }}
  client_id: {{ .Values.config.oidc.clientId | default "" | quote }}
  client_secret: {{ .Values.config.oidc.clientSecret | default "" | quote }}
  admin_client_id: {{ .Values.config.oidc.adminClientId | default "" | quote }}
  admin_client_secret: {{ .Values.config.oidc.adminClientSecret | default "" | quote }}
  scopes: ["openid", "profile", "email"]

session:
  secret_key: {{ required "config.session.secretKey must be a 32-byte secret" .Values.config.session.secretKey | quote }}
  cookie_name: "ns_session"
  idle_ttl: {{ .Values.config.session.idleTtl | quote }}
  absolute_ttl: {{ .Values.config.session.absoluteTtl | quote }}
  secure: {{ .Values.config.session.secure }}

tickets:
  replay_store: {{ $replayStore | quote }}
{{- if eq $replayStore "valkey" }}
  valkey:
{{- if .Values.valkey.enabled }}
    password: ""
    sentinel:
      master_name: {{ .Values.valkey.masterName | quote }}
      addresses:
{{- $root := . }}
{{- range $i := until (int .Values.valkey.sentinelReplicas) }}
        - {{ printf "%s-valkey-sentinel-%d.%s-valkey-sentinel:26379" (include "nereidsphere.fullname" $root) $i (include "nereidsphere.fullname" $root) | quote }}
{{- end }}
{{- else }}
    address: {{ .Values.valkey.external.address | quote }}
    password: {{ .Values.valkey.external.password | quote }}
    sentinel:
      master_name: {{ .Values.valkey.external.sentinelMasterName | quote }}
      password: {{ .Values.valkey.external.sentinelPassword | quote }}
      addresses:
{{ toYaml .Values.valkey.external.sentinelAddresses | indent 8 }}
{{- end }}
    dial_timeout: {{ .Values.tickets.dialTimeout | quote }}
    operation_timeout: {{ .Values.tickets.operationTimeout | quote }}
{{- end }}

gateway:
  cors_allowed_origins:
{{- if .Values.config.gateway.corsAllowedOrigins }}
{{ toYaml .Values.config.gateway.corsAllowedOrigins | indent 4 }}
{{- else }}
    []
{{- end }}
  requests_per_minute: {{ .Values.config.gateway.requestsPerMinute }}
  burst: {{ .Values.config.gateway.burst }}
  login_requests_per_minute: {{ .Values.config.gateway.loginRequestsPerMinute }}
  login_burst: {{ .Values.config.gateway.loginBurst }}
  upstream_timeout: {{ .Values.config.gateway.upstreamTimeout | quote }}

products:
{{- range .Values.config.products }}
  - id: {{ .id | quote }}
    name: {{ .name | quote }}
    description: {{ .description | quote }}
    url: {{ .url | quote }}
    mode: {{ .mode | default "ns" | quote }}
    oidc_client_id: {{ .oidcClientId | quote }}
    oidc_redirect_paths: ["/auth/callback", "/oidc/callback"]
    sso_callback_path: "/auth/ns-callback"
{{- if .tenantsPath }}
    tenants_path: {{ .tenantsPath | quote }}
{{- end }}
{{- if ne (.mode | default "ns") "oidc" }}
    sso_secret: {{ required "each mode=ns product needs an ssoSecret" .ssoSecret | quote }}
{{- end }}
{{- end }}
{{- end }}
