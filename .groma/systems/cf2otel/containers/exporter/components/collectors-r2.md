---
type: C4 Component
title: R2
status: stable
groma:
  id: collectors-r2
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/r2/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/r2/admission.go
      symbol: capResources
    - scanner: go
      file: internal/collectors/r2/metrics.go
  technology: Go
  group: Collector domains
description: R2 storage and operations metrics
---

Account-scoped R2 bandwidth, operations, storage, catalog and SQL metrics from GraphQL Groups datasets, with a cap on distinct attribute sets.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/r2/metrics.go](../../../../../../internal/collectors/r2/metrics.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
