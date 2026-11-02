---
type: Groma Flow
title: Poll and export a window
groma:
  id: poll-and-export-a-window
---

One scheduled poll of the sampled HTTP request collector, from startup to OTLP export. The checkpoint advances only after the flush succeeds, so a failed export retries the same window.

## Steps

| From | To | Action |
| --- | --- | --- |
| [Operator](../actors/operator.md) | [Entry point (cmd/cf2otel)](../systems/cf2otel/containers/exporter/components/entrypoint.md) | Start cf2otel with YAML and environment configuration |
| [Entry point (cmd/cf2otel)](../systems/cf2otel/containers/exporter/components/entrypoint.md) | [HTTP requests](../systems/cf2otel/containers/exporter/components/collectors-httpreq.md) | Register the enabled HTTP collectors |
| [Entry point (cmd/cf2otel)](../systems/cf2otel/containers/exporter/components/entrypoint.md) | [Collector runtime (internal/collector)](../systems/cf2otel/containers/exporter/components/collector.md) | Start the scheduler |
| [HTTP requests](../systems/cf2otel/containers/exporter/components/collectors-httpreq.md) | [Cloudflare API client (internal/cfapi)](../systems/cf2otel/containers/exporter/components/cfapi.md) | Query the window from the adaptive HTTP dataset |
| [Cloudflare API client (internal/cfapi)](../systems/cf2otel/containers/exporter/components/cfapi.md) | [Cloudflare API](../externals/cloudflare-api.md) | Send the rate-limited GraphQL request |
| [HTTP requests](../systems/cf2otel/containers/exporter/components/collectors-httpreq.md) | [Identity index (internal/identity)](../systems/cf2otel/containers/exporter/components/identity.md) | Attach an inferred Access identity |
| [Collector runtime (internal/collector)](../systems/cf2otel/containers/exporter/components/collector.md) | [Telemetry (internal/telemetry)](../systems/cf2otel/containers/exporter/components/telemetry.md) | Buffer the whole window and flush it |
| [Telemetry (internal/telemetry)](../systems/cf2otel/containers/exporter/components/telemetry.md) | [OTLP backend](../externals/otlp-backend.md) | Export logs and metrics |
