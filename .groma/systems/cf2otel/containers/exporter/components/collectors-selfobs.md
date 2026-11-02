---
type: C4 Component
title: Self-observability
status: stable
groma:
  id: collectors-selfobs
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/selfobs/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/selfobs/error_class.go
      symbol: errorClass
    - scanner: go
      file: internal/collectors/selfobs/polling.go
    - scanner: go
      file: internal/collectors/selfobs/selfobs.go
  technology: Go
  group: Collector domains
description: cf2otel's own polling health
---

Snapshot collector for exporter self-observability: per-collector scrape outcomes, elapsed time, checkpoint backlog against actual registered intervals, and bounded scrape error classes derived from the Cloudflare client's typed errors.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/selfobs/error_class.go](../../../../../../internal/collectors/selfobs/error_class.go) | [internal/cfapi/graphql.go](../../../../../../internal/cfapi/graphql.go) | Classifies API errors | Go error types |
