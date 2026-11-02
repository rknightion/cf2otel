---
type: C4 Component
title: Load balancers
status: stable
groma:
  id: collectors-loadbalancers
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/loadbalancers/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/loadbalancers/health.go
    - scanner: go
      file: internal/collectors/loadbalancers/requests.go
  technology: Go
  group: Collector domains
description: Load balancer pool health and requests
---

Disabled-by-default snapshot of documented pool health flags and pool request counts.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/loadbalancers/health.go](../../../../../../internal/collectors/loadbalancers/health.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
