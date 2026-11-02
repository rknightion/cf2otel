---
type: C4 Component
title: Health endpoint (internal/health)
status: stable
groma:
  id: health
  parent: exporter
  code:
    - scanner: go
      file: internal/health/health.go
  technology: Go, HTTP
  group: Process and endpoints
description: Loopback health endpoint and probe
---

Serves `/healthz` on a loopback-only listener without exposing configuration, and provides the probe that `--healthcheck` uses so a container runtime can check the process.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/health/health.go](../../../../../../internal/health/health.go) | [internal/config/config.go](../../../../../../internal/config/config.go) | Reads listen address | Go types |
