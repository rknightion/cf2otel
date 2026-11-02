---
type: C4 Component
title: Logpush
status: stable
groma:
  id: collectors-logpush
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/logpush/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/logpush/failures.go
    - scanner: go
      file: internal/collectors/logpush/health.go
  technology: Go
  group: Collector domains
description: Logpush job health and failures
---

Logpush upload health and failure counts from Groups datasets over complete five-minute buckets; job labels on failures require opt-in.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/logpush/failures.go](../../../../../../internal/collectors/logpush/failures.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
