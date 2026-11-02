---
type: C4 Component
title: Health checks
status: stable
groma:
  id: collectors-healthchecks
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/healthchecks/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/healthchecks/events.go
  technology: Go
  group: Collector domains
description: Zone health-check analytics
---

Health-check analytics, installed only after the operator opts in to origin labels.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/healthchecks/events.go](../../../../../../internal/collectors/healthchecks/events.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
