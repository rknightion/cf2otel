---
type: C4 Component
title: Gateway DNS
status: stable
groma:
  id: collectors-gateway
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/gateway/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/gateway/dns.go
  technology: Go
  group: Collector domains
description: Gateway DNS metrics
---

Zero Trust Gateway DNS query metrics from a GraphQL Groups dataset, when enabled.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/gateway/dns.go](../../../../../../internal/collectors/gateway/dns.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
