---
type: C4 System
title: Prometheus scraper
status: stable
groma:
  id: prometheus-scraper
  technology: HTTP
---

Any Prometheus-compatible scraper that pulls the optional, unauthenticated metrics endpoint.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [prometheus-scraper](prometheus-scraper.md) | [internal/promexport/promexport.go](../../internal/promexport/promexport.go) | Scrapes metrics | HTTP |
