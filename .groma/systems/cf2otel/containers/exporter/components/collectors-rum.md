---
type: C4 Component
title: Web Analytics (RUM)
status: stable
groma:
  id: collectors-rum
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/rum/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/rum/rum.go
  technology: Go
  group: Collector domains
description: Web Analytics page-load and Web Vitals metrics
---

Account-level Web Analytics (RUM) page loads and Web Vitals from GraphQL Groups datasets.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/rum/rum.go](../../../../../../internal/collectors/rum/rum.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
