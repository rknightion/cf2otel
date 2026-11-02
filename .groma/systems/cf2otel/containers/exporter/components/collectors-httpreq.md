---
type: C4 Component
title: HTTP requests
status: stable
groma:
  id: collectors-httpreq
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/httpreq/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/httpreq/breakdowns.go
    - scanner: go
      file: internal/collectors/httpreq/high_cardinality.go
    - scanner: go
      file: internal/collectors/httpreq/http.go
    - scanner: go
      file: internal/collectors/httpreq/kpis.go
    - scanner: go
      file: internal/collectors/httpreq/latency.go
  technology: Go
  group: Collector domains
description: Sampled HTTP request events and Groups-based HTTP metrics
---

Per-request events from the adaptive (sampled) HTTP dataset, with any Access user identity inferred through the identity index and flagged as inferred, plus HTTP metrics, threats and transfer series computed from `*Groups` datasets so rates stay correct under sampling. Optional breakdowns add bounded dimensions.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/httpreq/http.go](../../../../../../internal/collectors/httpreq/http.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
| [internal/collectors/httpreq/http.go](../../../../../../internal/collectors/httpreq/http.go) | [internal/identity/index.go](../../../../../../internal/identity/index.go) | Looks up identity | Go call |
| [internal/collectors/httpreq/http.go](../../../../../../internal/collectors/httpreq/http.go) | [internal/collectors/access/logins.go](../../../../../../internal/collectors/access/logins.go) | Hydrates identity | Go call |
