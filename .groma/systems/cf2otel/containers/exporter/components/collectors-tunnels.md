---
type: C4 Component
title: Tunnels
status: stable
groma:
  id: collectors-tunnels
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/tunnels/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/tunnels/status.go
  technology: Go
  group: Collector domains
description: Cloudflare Tunnel status
---

Opt-in snapshot of tunnel status, emitting a status-change event when a tunnel's status differs from the previous poll.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/tunnels/status.go](../../../../../../internal/collectors/tunnels/status.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
