---
type: C4 Component
title: Entry point (cmd/cf2otel)
status: stable
groma:
  id: entrypoint
  parent: exporter
  code:
    - scanner: go
      file: cmd/cf2otel/main.go
    - scanner: go
      file: cmd/cf2otel/collectors_import.go
      symbol: registerCollectors
  technology: Go
  group: Process and endpoints
description: Process wiring and the frozen collector domain list
---

`cmd/cf2otel` owns process lifecycle: parse flags, load and validate configuration, set up logging, OTLP providers, the request-observed Cloudflare client, the checkpoint store, the optional Access identity index and the optional health and Prometheus endpoints, then register collectors and run the scheduler until shutdown. `collectors_import.go` is the frozen domain call list: each domain's `Register` is called in a fixed order there and nowhere else.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/cli/cli.go](../../../../../../internal/cli/cli.go) | Parses flags | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/config/config.go](../../../../../../internal/config/config.go) | Loads configuration | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/telemetry/provider.go](../../../../../../internal/telemetry/provider.go) | Builds OTLP providers | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Builds API client | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/identity/index.go](../../../../../../internal/identity/index.go) | Builds identity index | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/collector/scheduler.go](../../../../../../internal/collector/scheduler.go) | Runs scheduler | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/health/health.go](../../../../../../internal/health/health.go) | Serves health | Go call |
| [cmd/cf2otel/main.go](../../../../../../cmd/cf2otel/main.go) | [internal/promexport/promexport.go](../../../../../../internal/promexport/promexport.go) | Starts pull endpoint | Go call |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/access/register.go](../../../../../../internal/collectors/access/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/aigateway/register.go](../../../../../../internal/collectors/aigateway/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/audit/register.go](../../../../../../internal/collectors/audit/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/certs/register.go](../../../../../../internal/collectors/certs/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/d1/register.go](../../../../../../internal/collectors/d1/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/dex/register.go](../../../../../../internal/collectors/dex/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/dns/register.go](../../../../../../internal/collectors/dns/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/durableobjects/register.go](../../../../../../internal/collectors/durableobjects/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/email/register.go](../../../../../../internal/collectors/email/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/firewall/register.go](../../../../../../internal/collectors/firewall/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/gateway/register.go](../../../../../../internal/collectors/gateway/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/healthchecks/register.go](../../../../../../internal/collectors/healthchecks/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/httpreq/register.go](../../../../../../internal/collectors/httpreq/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/inventory/register.go](../../../../../../internal/collectors/inventory/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/kv/register.go](../../../../../../internal/collectors/kv/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/loadbalancers/register.go](../../../../../../internal/collectors/loadbalancers/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/logpush/register.go](../../../../../../internal/collectors/logpush/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/queues/register.go](../../../../../../internal/collectors/queues/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/r2/register.go](../../../../../../internal/collectors/r2/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/rum/register.go](../../../../../../internal/collectors/rum/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/selfobs/register.go](../../../../../../internal/collectors/selfobs/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/statuspage/register.go](../../../../../../internal/collectors/statuspage/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/tunnels/register.go](../../../../../../internal/collectors/tunnels/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/turnstile/register.go](../../../../../../internal/collectors/turnstile/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/warp/fleet.go](../../../../../../internal/collectors/warp/fleet.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/workers/register.go](../../../../../../internal/collectors/workers/register.go) | Registers collectors | Register(collector.Deps) |
| [cmd/cf2otel/collectors_import.go](../../../../../../cmd/cf2otel/collectors_import.go) | [internal/collectors/workersai/register.go](../../../../../../internal/collectors/workersai/register.go) | Registers collectors | Register(collector.Deps) |
