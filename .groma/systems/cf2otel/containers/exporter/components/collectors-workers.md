---
type: C4 Component
title: Workers
status: stable
groma:
  id: collectors-workers
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/workers/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/workers/invocations.go
    - scanner: go
      file: internal/collectors/workers/overview.go
  technology: Go
  group: Collector domains
description: Workers overview and invocation metrics
---

Workers overview and invocation metrics from GraphQL analytics.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/workers/overview.go](../../../../../../internal/collectors/workers/overview.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
