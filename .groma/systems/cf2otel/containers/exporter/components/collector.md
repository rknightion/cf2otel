---
type: C4 Component
title: Collector runtime (internal/collector)
status: stable
groma:
  id: collector
  parent: exporter
  code:
    - scanner: go
      file: internal/collector/collector.go
    - scanner: go
      file: internal/collector/checkpoint.go
    - scanner: go
      file: internal/collector/scheduler.go
    - scanner: go
      file: internal/collector/splitter.go
    - scanner: go
      file: internal/collector/zones.go
  technology: Go
  group: Collection core
description: Collector registration, scheduling, windows and checkpoints
---

Defines the snapshot and window collector interfaces and the `Deps` every domain's `Register` receives. The scheduler runs each registered collector on its own interval; window collectors get half-open time windows bounded by lookback, maximum width and source lag, split by bisection when a source reports saturation. A checkpoint advances only after the window's OTLP flush succeeds, and is stored atomically in a JSON file. Also owns zone selection and the per-poll zone gauges.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collector/scheduler.go](../../../../../../internal/collector/scheduler.go) | [internal/telemetry/buffer.go](../../../../../../internal/telemetry/buffer.go) | Buffers and commits windows | Go call |
| [internal/collector/scheduler.go](../../../../../../internal/collector/scheduler.go) | [internal/semconv/selfobs.go](../../../../../../internal/semconv/selfobs.go) | Names window signals | Go constants |
| [internal/collector/zones.go](../../../../../../internal/collector/zones.go) | [internal/cfapi/types.go](../../../../../../internal/cfapi/types.go) | Selects zones | Go types |
