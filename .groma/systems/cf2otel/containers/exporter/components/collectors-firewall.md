---
type: C4 Component
title: Firewall
status: stable
groma:
  id: collectors-firewall
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/firewall/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/firewall/dimensions.go
    - scanner: go
      file: internal/collectors/firewall/events.go
    - scanner: go
      file: internal/collectors/firewall/firewall.go
    - scanner: go
      file: internal/collectors/firewall/metrics.go
  technology: Go
  group: Collector domains
description: Firewall security events and metrics
---

Per-request security actions from the adaptive firewall dataset, and Groups-only firewall metrics, with ruleset lookups for readable dimensions.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/firewall/events.go](../../../../../../internal/collectors/firewall/events.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
