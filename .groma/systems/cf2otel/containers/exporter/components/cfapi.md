---
type: C4 Component
title: Cloudflare API client (internal/cfapi)
status: stable
groma:
  id: cfapi
  parent: exporter
  code:
    - scanner: go
      file: internal/cfapi/client.go
    - scanner: go
      file: internal/cfapi/batch.go
    - scanner: go
      file: internal/cfapi/entitlement_backoff.go
      symbol: settingsFor
    - scanner: go
      file: internal/cfapi/graphql.go
    - scanner: go
      file: internal/cfapi/limiter.go
    - scanner: go
      file: internal/cfapi/ratelimit_headers.go
    - scanner: go
      file: internal/cfapi/request_observer.go
    - scanner: go
      file: internal/cfapi/types.go
  technology: Go, HTTPS, GraphQL
  group: Collection core
description: The only package that talks to Cloudflare, read-only by construction
---

One HTTP client for the Cloudflare REST and GraphQL Analytics APIs. It supports read requests only. It paginates REST lists, negotiates GraphQL fields per dataset from `settings.availableFields` (one unentitled field fails a whole query), respects `maxNumberOfFields`, `maxDuration` and `notOlderThan`, batches GraphQL selections, and reports saturation, retention gaps and entitlement errors as typed errors. A shared limiter keeps REST and GraphQL inside the token's rate budget and backs off on rate-limit headers.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | [internal/config/config.go](../../../../../../internal/config/config.go) | Reads API settings | Go types |
| [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | [cloudflare-api](../../../../../externals/cloudflare-api.md) | Reads analytics and logs | HTTPS REST, GraphQL |
