---
type: C4 Component
title: D1
status: stable
groma:
  id: collectors-d1
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/d1/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/d1/d1.go
    - scanner: go
      file: internal/collectors/d1/depth.go
    - scanner: go
      file: internal/collectors/d1/named_metrics.go
      symbol: aggregateNamed
    - scanner: go
      file: internal/collectors/d1/names.go
  technology: Go
  group: Collector domains
description: D1 database metrics
---

Account-level D1 analytics, query and storage metrics from GraphQL Groups datasets, with database names resolved over REST.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/d1/d1.go](../../../../../../internal/collectors/d1/d1.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
