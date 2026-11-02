---
type: C4 Component
title: Durable Objects
status: stable
groma:
  id: collectors-durableobjects
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/durableobjects/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/durableobjects/datasets.go
    - scanner: go
      file: internal/collectors/durableobjects/depth.go
    - scanner: go
      file: internal/collectors/durableobjects/named_metrics.go
      symbol: aggregateNamed
    - scanner: go
      file: internal/collectors/durableobjects/names.go
  technology: Go
  group: Collector domains
description: Durable Objects metrics
---

Durable Objects invocation, periodic, SQL storage and subrequest metrics. Each dataset is its own window collector so each has an independent checkpoint.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/durableobjects/datasets.go](../../../../../../internal/collectors/durableobjects/datasets.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
