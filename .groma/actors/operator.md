---
type: C4 Actor
title: Operator
status: stable
groma:
  id: operator
---

Deploys and configures cf2otel (YAML plus `CF2OTEL_` environment variables, including the Cloudflare API token and OTLP credentials) and watches its self-observability signals.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [operator](operator.md) | [cmd/cf2otel/main.go](../../cmd/cf2otel/main.go) | Runs and configures | CLI, YAML, environment |
