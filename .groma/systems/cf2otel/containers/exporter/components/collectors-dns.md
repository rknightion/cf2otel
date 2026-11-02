---
type: C4 Component
title: DNS
status: stable
groma:
  id: collectors-dns
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/dns/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/dns/dns.go
  technology: Go
  group: Collector domains
description: DNS query events and metrics
---

Per-query DNS events from the adaptive DNS analytics dataset, and Groups-based DNS metrics.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/dns/dns.go](../../../../../../internal/collectors/dns/dns.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
