---
type: C4 Component
title: DEX
status: stable
groma:
  id: collectors-dex
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/dex/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/dex/tests.go
  technology: Go
  group: Collector domains
description: DEX test result averages
---

Disabled-by-default snapshot of bounded Digital Experience Monitoring test interval averages.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/dex/tests.go](../../../../../../internal/collectors/dex/tests.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
