---
type: C4 Component
title: Queues
status: stable
groma:
  id: collectors-queues
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/queues/register.go
    - scanner: go
      file: internal/collectors/queues/depth.go
    - scanner: go
      file: internal/collectors/queues/named_metrics.go
      symbol: aggregateNamed
    - scanner: go
      file: internal/collectors/queues/names.go
  technology: Go
  group: Collector domains
description: Queues backlog and message metrics
---

Queue backlog, delayed backlog, consumer and message-operation metrics, with queue names resolved over REST.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/queues/depth.go](../../../../../../internal/collectors/queues/depth.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
