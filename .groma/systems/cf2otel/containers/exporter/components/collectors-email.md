---
type: C4 Component
title: Email
status: stable
groma:
  id: collectors-email
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/email/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/email/metrics.go
  technology: Go
  group: Collector domains
description: Email Routing and Email Sending metrics
---

Zone-scoped Email Routing and Email Sending Groups counts summed across account-owned zones over complete five-minute buckets, without zone attributes.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/email/metrics.go](../../../../../../internal/collectors/email/metrics.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
