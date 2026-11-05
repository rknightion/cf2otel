---
type: C4 Component
title: AI Gateway
status: stable
groma:
  id: collectors-aigateway
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/aigateway/register.go
    - scanner: go
      file: internal/collectors/aigateway/coverage_gap.go
    - scanner: go
      file: internal/collectors/aigateway/logs_content.go
    - scanner: go
      file: internal/collectors/aigateway/logs_fetch.go
    - scanner: go
      file: internal/collectors/aigateway/logs_model.go
    - scanner: go
      file: internal/collectors/aigateway/logs_usage.go
    - scanner: go
      file: internal/collectors/aigateway/logs.go
    - scanner: go
      file: internal/collectors/aigateway/logs_cache.go
  technology: Go
  group: Collector domains
description: AI Gateway requests as GenAI spans, logs and metrics
---

Reads AI Gateway REST logs and emits request events, OpenTelemetry GenAI client spans and metrics, with optional capped request and response content. GraphQL metrics are deliberately not registered while their ingestion lag is unbounded; an optional coverage collector compares REST row counts against Groups counts.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/aigateway/logs_fetch.go](../../../../../../internal/collectors/aigateway/logs_fetch.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
