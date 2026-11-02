---
type: C4 Component
title: Audit log
status: stable
groma:
  id: collectors-audit
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/audit/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/audit/audit.go
  technology: Go
  group: Collector domains
description: Account audit log events
---

Windows over the account audit log, emitting one event per entry; emails and IPs stay on log attributes.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/audit/audit.go](../../../../../../internal/collectors/audit/audit.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
