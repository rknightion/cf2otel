---
type: C4 Component
title: Workers AI
status: stable
groma:
  id: collectors-workersai
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/workersai/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/workersai/metrics.go
  technology: Go
  group: Collector domains
description: Workers AI aggregate metrics
---

Opt-in Workers AI aggregate metrics from batched GraphQL queries.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/workersai/metrics.go](../../../../../../internal/collectors/workersai/metrics.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
