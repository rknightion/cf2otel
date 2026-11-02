---
type: C4 Component
title: Certificate packs
status: stable
groma:
  id: collectors-certs
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/certs/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/certs/packs.go
  technology: Go
  group: Collector domains
description: Certificate-pack snapshot
---

Opt-in snapshot of zone SSL certificate packs from the REST API.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/certs/packs.go](../../../../../../internal/collectors/certs/packs.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
