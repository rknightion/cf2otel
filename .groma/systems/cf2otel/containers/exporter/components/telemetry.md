---
type: C4 Component
title: Telemetry (internal/telemetry)
status: stable
groma:
  id: telemetry
  parent: exporter
  code:
    - scanner: go
      file: internal/telemetry/emitter.go
    - scanner: go
      file: internal/telemetry/api_observer.go
      symbol: APIRequestObserver
    - scanner: go
      file: internal/telemetry/buffer.go
    - scanner: go
      file: internal/telemetry/cardinality.go
    - scanner: go
      file: internal/telemetry/export_observer.go
    - scanner: go
      file: internal/telemetry/filter.go
    - scanner: go
      file: internal/telemetry/log_batch.go
    - scanner: go
      file: internal/telemetry/noop.go
    - scanner: go
      file: internal/telemetry/process_log.go
    - scanner: go
      file: internal/telemetry/provider.go
    - scanner: go
      file: internal/telemetry/snapshot.go
  technology: Go, OpenTelemetry Go SDK, OTLP
  group: Collection core
description: The only package that touches OTLP
---

Builds the OTLP log, metric and trace providers (HTTP or gRPC) and the Emitter interface collectors write through. A window is buffered completely before any SDK queue is touched, then flushed as one commit so the scheduler knows whether export succeeded. Applies the operator's metric and attribute deny-lists, guards metric cardinality, publishes gauge snapshots, repairs invalid UTF-8 at emission, tees the process log to OTLP as `cf2otel.process.log`, and records Cloudflare API request and export health as self-metrics.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/telemetry/api_observer.go](../../../../../../internal/telemetry/api_observer.go) | [internal/cfapi/request_observer.go](../../../../../../internal/cfapi/request_observer.go) | Observes API requests | Go callback |
| [internal/telemetry/emitter.go](../../../../../../internal/telemetry/emitter.go) | [internal/semconv/semconv.go](../../../../../../internal/semconv/semconv.go) | Applies signal names | Go constants |
| [internal/telemetry/provider.go](../../../../../../internal/telemetry/provider.go) | [otlp-backend](../../../../../externals/otlp-backend.md) | Exports signals | OTLP HTTP or gRPC |
