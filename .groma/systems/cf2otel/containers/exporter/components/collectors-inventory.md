---
type: C4 Component
title: Access inventory
status: stable
groma:
  id: collectors-inventory
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/inventory/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/inventory/inventory.go
  technology: Go
  group: Collector domains
description: Access application and user inventory
---

Snapshot of Access applications (one gauge per app, with name and type) and the Access user count, from the REST API.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/inventory/inventory.go](../../../../../../internal/collectors/inventory/inventory.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
