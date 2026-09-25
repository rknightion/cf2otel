# Configuration

Settings load in this order: built-in defaults, YAML, then `CF2OTEL_` environment variables. Use `__` between nested keys. The [generated environment variable reference](env-vars.md) lists the struct-backed keys.

| Section | Important settings |
| --- | --- |
| `cloudflare` | `account_id`, optional `zones`, API base, timeout and response limit. `api_token` is environment-only. |
| `collectors` | `enabled`, `interval`, `initial_lookback` and `max_window` per named collector. |
| `access` | `include_service_tokens` keeps service-token activity separate from human logins. |
| `http` | `scope` controls request events and defaults to `access_protected`. `metrics_scope` inherits it by default; set `metrics_scope: all` to collect Groups metrics across account zones without widening events. `hosts` is required for either `hosts` scope. `max_metric_hosts_per_zone` (1000) and `max_metric_series_per_window` (10000) reject oversized metric windows before checkpoint advance. |
| `identity` | Enable the inferred login-to-request join and bound its `match_window` and `max_candidates`. |
| `platform` | `max_metric_series_per_window` defaults to 500 per collector window. Excess series are dropped in sorted-name order with a count-only warning; source resource IDs never become metric attributes. |
| `ai_gateway` | Select `gateways`, opt in to `capture_bodies`, cap each body with `max_body_bytes`, and link caller traces when headers allow. |
| `otlp` | `endpoint`, `protocol` (`http` or `grpc`), Grafana Cloud instance ID and environment-only token or headers. |
| `state` | Persistent checkpoint directory; default `/var/lib/cf2otel`. |
| `health`, `log` | Loopback health listener and application logging. |

Collector keys are `access.logins`, `access.login_metrics`, `access.scim`, `inventory.access`, `httpreq.events`, `httpreq.metrics`, `aigateway.logs`, `aigateway.metrics`, `audit.logs`, `firewall.events`, `firewall.metrics`, `dns.events`, `dns.metrics`, `rum.pageloads`, `rum.web_vitals`, `gateway.dns`, `workers.overview`, `turnstile.events`, `logpush.health`, `d1.analytics`, `d1.queries`, `d1.storage`, `kv.operations`, `kv.storage`, `r2.bandwidth`, `r2.catalog_data`, `r2.catalog_maintenance`, `r2.operations`, `r2.storage`, `r2.sql`, `durableobjects.invocations`, `durableobjects.periodic`, `durableobjects.sql_storage`, `durableobjects.subrequests`, `queues.backlog`, `queues.consumer`, `queues.delayed_backlog`, `queues.message_operations`, and `selfobs`. Enabled collectors default to five-minute intervals. `aigateway.metrics` is disabled and unscheduled because its GraphQL Groups ingestion lag is not bounded; `aigateway.logs` emits the AI Gateway metrics from REST rows. The default initial lookback is 30 minutes and maximum window is one hour. The scheduler advances a checkpoint only after a successful window.

The Access REST log has only about a day's observed reach. Keep its polling interval short and preserve the state directory; a long outage cannot be repaired by expanding the lookback. Cloudflare GraphQL retention and permitted window width vary by dataset and plan, so cf2otel negotiates available fields and splits requests to fit reported limits.

`httpRequestsAdaptive` event rows are sampled. Use the companion `httpreq.metrics` collector for corrected aggregate counts; do not count event rows to calculate a request rate.

See [Security and PII](security.md) before enabling AI Gateway body capture or wider HTTP scope.

## Delivery semantics

Collected logs and spans have at-least-once delivery when their window is ultimately committed. The
scheduler advances a window checkpoint after a successful commit or after it drops the window
following three payload rejections. Other failed commits leave the checkpoint unchanged and allow
the window to be retried. The scheduler flushes windows in chunks; if a later chunk fails, retrying
the uncommitted window can resend records from chunks the backend already accepted. OTLP partial
success is reported as an export error, so accepted records in that response can also be sent again
when the window is retried. Records rejected by OTLP can therefore be lost. cf2otel does not attach
a universal delivery ID or guarantee exactly-once delivery.

Use the event family and these source fields as deduplication keys. `event_name` identifies the log
family; the record timestamp is the OTLP log timestamp. For a key that includes multiple fields,
keep each field as a separate tuple element rather than concatenating ambiguous strings. When a
listed source ID is absent, use a deterministic hash of the event name, timestamp, body and complete
set of event attributes. The `EventGenAIContent` span event is part of its parent AI Gateway request
span; its content logs use the content side to distinguish request and response records.

| Event constant | Signal use | Dedupe key |
| --- | --- | --- |
| `EventAccessLogin` | Log | `cloudflare.access.ray_id`, record timestamp and all emitted event attributes. A ray ID and timestamp can identify more than one login row. |
| `EventAccessSCIM` | Log | `cloudflare.access.scim.idp_id`, `cloudflare.access.scim.resource_type`, `cloudflare.access.scim.resource_id`, record timestamp, `cloudflare.access.scim.method`, `cloudflare.access.scim.status` and `cloudflare.access.scim.user_email`. |
| `EventAuditEvent` | Log | `cloudflare.audit.id`. |
| `EventAIGatewayRequest` | Log | `cloudflare.ai_gateway.gateway.name` and `cloudflare.ai_gateway.log.id`. |
| `EventGenAIContent` | Content log and span event | Content logs: `cloudflare.ai_gateway.gateway.name`, `cloudflare.ai_gateway.log.id` and `cloudflare.ai_gateway.content.side`. Span event: parent request identity, `cloudflare.ai_gateway.gateway.name` and `cloudflare.ai_gateway.log.id`. |
| `EventHTTPRequest` | Log | `cloudflare.http.zone`, `cloudflare.http.ray_id` and record timestamp. |
| `EventFirewallEvent` | Log | `cloudflare.firewall.zone`, `cloudflare.firewall.ray_id` and record timestamp. |
| `EventDNSQuery` | Log | `cloudflare.dns.zone`, record timestamp and a deterministic hash of the complete event body and attributes; the source row has no event ID, so this is best-effort and cannot distinguish identical queries. Do not rely on it for exact query counts. |
| `EventWindowGap` | Log | `cf2otel.collector`, `cf2otel.window.from`, `cf2otel.window.floor` and `cloudflare.dns.zone` when present. |

cf2otel also emits two span families. Deduplicate the AI Gateway request span by
`cloudflare.ai_gateway.gateway.name` and `cloudflare.ai_gateway.log.id`. Deduplicate the
`cf2otel.api.request` span by its OpenTelemetry `trace_id` and `span_id`; each observed API call is a
separate span.
