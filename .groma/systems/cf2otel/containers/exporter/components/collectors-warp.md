---
type: C4 Component
title: WARP fleet
status: stable
groma:
  id: collectors-warp
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/warp/fleet.go
  technology: Go
  group: Collector domains
description: Recently seen WARP fleet counts
---

Opt-in snapshot counting recently seen WARP devices from the DEX fleet-status API, bounded by a series cap.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/warp/fleet.go](../../../../../../internal/collectors/warp/fleet.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
