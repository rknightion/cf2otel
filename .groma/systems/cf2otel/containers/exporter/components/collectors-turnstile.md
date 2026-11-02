---
type: C4 Component
title: Turnstile
status: stable
groma:
  id: collectors-turnstile
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/turnstile/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/turnstile/events.go
  technology: Go
  group: Collector domains
description: Turnstile analytics
---

Turnstile event counts over complete five-minute buckets, when enabled.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/turnstile/events.go](../../../../../../internal/collectors/turnstile/events.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
