---
type: C4 Component
title: Status page
status: stable
groma:
  id: collectors-statuspage
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/statuspage/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/statuspage/components.go
    - scanner: go
      file: internal/collectors/statuspage/incidents.go
    - scanner: go
      file: internal/collectors/statuspage/source.go
  technology: Go
  group: Collector domains
description: Public Cloudflare status components and incidents
---

Opt-in collectors for the public vendor status page: component status gauges and incident-update events. Deliberately uses its own unauthenticated HTTP client, not the cfapi client, so no tenant token reaches it.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/statuspage/source.go](../../../../../../internal/collectors/statuspage/source.go) | [cloudflare-status-page](../../../../../externals/cloudflare-status-page.md) | Reads public status | HTTPS JSON |
