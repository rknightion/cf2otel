---
type: C4 Component
title: Workers KV
status: stable
groma:
  id: collectors-kv
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/kv/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/kv/kv.go
    - scanner: go
      file: internal/collectors/kv/named_metrics.go
      symbol: aggregateNamed
    - scanner: go
      file: internal/collectors/kv/names.go
  technology: Go
  group: Collector domains
description: Workers KV metrics
---

Account-level KV operation and storage metrics, with namespace names resolved over REST.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/kv/names.go](../../../../../../internal/collectors/kv/names.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
