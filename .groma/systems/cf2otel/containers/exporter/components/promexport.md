---
type: C4 Component
title: Prometheus endpoint (internal/promexport)
status: stable
groma:
  id: promexport
  parent: exporter
  code:
    - scanner: go
      file: internal/promexport/promexport.go
  technology: Go, Prometheus client
  group: Process and endpoints
description: Opt-in Prometheus pull endpoint
---

An optional metrics pull endpoint backed by a private Prometheus registry, attached as a reader on the OTLP metric provider. It has no authentication or TLS and is off by default.
