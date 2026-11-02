---
type: C4 Component
title: Config (internal/config)
status: stable
groma:
  id: config
  parent: exporter
  code:
    - scanner: go
      file: internal/config/config.go
  technology: Go, koanf
  group: Collection core
description: The koanf configuration surface
---

Loads defaults, then YAML, then `CF2OTEL_` environment variables, with double underscores for nesting (for example `CF2OTEL_CLOUDFLARE__API_TOKEN`). Secrets are environment-only and rejected if found in YAML. Holds per-collector enablement, interval, initial lookback and maximum window, plus OTLP, health, state and deny-list settings. Every other package reads its settings from here.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/config/config.go](../../../../../../internal/config/config.go) | [internal/semconv/deny_registry.go](../../../../../../internal/semconv/deny_registry.go) | Validates deny-lists | Go call |
