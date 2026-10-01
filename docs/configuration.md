# Configuration

Settings load in this order: built-in defaults, YAML, then `CF2OTEL_` environment variables. Use `__` between nested keys. The [generated environment variable reference](env-vars.md) lists the struct-backed keys. For collectors, use `CF2OTEL_COLLECTORS__<NAME>__<SETTING>`, replacing dots in the collector name with underscores (for example, `CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__ENABLED=true`). Settings are `ENABLED`, `INTERVAL`, `INITIAL_LOOKBACK` and `MAX_WINDOW`; environment values override YAML.

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

Collector keys are `access.logins`, `access.login_metrics`, `access.scim`, `inventory.access`, `httpreq.events`, `httpreq.metrics`, `aigateway.logs`, `aigateway.metrics`, `aigateway.coverage`, `audit.logs`, `firewall.events`, `firewall.metrics`, `dns.events`, `dns.metrics`, `rum.pageloads`, `rum.web_vitals`, `gateway.dns`, `workers.overview`, `workers.invocations`, `turnstile.events`, `logpush.health`, `d1.analytics`, `d1.queries`, `d1.storage`, `kv.operations`, `kv.storage`, `r2.bandwidth`, `r2.catalog_data`, `r2.catalog_maintenance`, `r2.operations`, `r2.storage`, `r2.sql`, `durableobjects.invocations`, `durableobjects.periodic`, `durableobjects.sql_storage`, `durableobjects.subrequests`, `queues.backlog`, `queues.consumer`, `queues.delayed_backlog`, `queues.message_operations`, `email.routing`, `email.sending`, `selfobs`, `certs.packs`, and `tunnels.status`. Enabled collectors default to five-minute intervals. The email collectors sum Groups counts across account-owned zones over complete five-minute buckets; they emit no zone metric attributes. DMARC is excluded. `aigateway.metrics` is disabled and unscheduled because its GraphQL Groups ingestion lag is not bounded; `aigateway.logs` emits the AI Gateway metrics from REST rows. The default initial lookback is 30 minutes and maximum window is one hour. `aigateway.coverage` is present but disabled by default. The scheduler advances a checkpoint after a successful window or after it drops a window following three payload rejections.

The Access REST log has only about a day's observed reach. Keep its polling interval short and preserve the state directory; a long outage cannot be repaired by expanding the lookback. Cloudflare GraphQL retention and permitted window width vary by dataset and plan, so cf2otel negotiates available fields and splits requests to fit reported limits.

`httpRequestsAdaptive` event rows are sampled. Use the companion `httpreq.metrics` collector for corrected aggregate counts; do not count event rows to calculate a request rate.

See [Security and PII](security.md) before enabling AI Gateway body capture or wider HTTP scope.

## Additional analytics settings

| Key | Default | Contract |
| --- | --- | --- |
| `otlp.metric_cardinality_limit` | `10000` | Nonnegative integer; `0` means no limit. The application maps config `0` to provider option `-1`; a zero-valued provider option retains the SDK default for existing callers. Independently, DNS uses a lifetime series budget of `9999`, or `min(9999, max(1, limit-1))` for a positive configured limit, including one reserved coarse fallback series (see signals). Config `0` does not disable that DNS budget. Explicit limit `1` cannot avoid the SDK's reserved overflow series; SDK overflow observability remains active. (CFO-0045) |
| `http.request_source` | `eyeball` | `eyeball` or `all`; applies only to `httpreq.metrics` Groups queries, never raw events. Eyeball-only totals exclude internal traffic and Worker subrequests. (CFO-0046.01) |
| `http.breakdowns` | `[status, origin_status, country, protocol, tls_protocol, method, content_type]` | Each value must be in this set and enables one attribute. An instrument is emitted when any of its attributes is enabled and carries only enabled attributes. `[]` disables breakdown instruments. Breakdowns are zone-level, without host. (CFO-0046.02) |
| `collectors.logpush.failures` | Disabled, `5m` interval, `30m` lookback, `1h` maximum window | Explicit opt-in to bounded numeric job-ID labels at account and zone scope, subject to the platform series cap. Existing `logpush.health` stays unchanged. Environment form: `LOGPUSH_FAILURES`. (CFO-0047.03) |
| `collectors.httpreq.threats` | Enabled, `1h` interval/lookback/maximum window | Complete UTC-hour rollups with at least ten minutes of holdback; Free/Pro supported. Threat rollups are not assumed eyeball-filterable. Environment form: `HTTPREQ_THREATS`. (CFO-0046.05) |
| `collectors.httpreq.transfer` | Enabled, `1h` interval, zero lookback/window | Account snapshot without checkpoint; UTC month-to-date bytes and exact UTC-month linear projection, always eyeball-only regardless of `http.request_source`. Month window is at most 31 days, within the observed 32-day account settings. Environment form: `HTTPREQ_TRANSFER`. (CFO-0046.05) |
| `collectors.healthchecks.events` | Disabled, `5m` interval, `30m` lookback, `1h` maximum window | Pro-only explicit opt-in to bounded origin labels; disabled Free datasets skipped. Timing identity is non-IP FQDN or human health-check name, at most 128 characters; omit timings without usable identity. Environment form: `HEALTHCHECKS_EVENTS`. Registration is an intermediate no-op until the collector lands. (CFO-0050.03) |
| `httpreq.metrics` visits extension | Existing defaults unchanged | Optional advertised `sum_visits` follows the existing request-source policy; no separate collector key. All extension config and signal declarations prepare independent collector implementations, not new output in this seam-only change. (CFO-0046.05) |
| `collectors.workers.invocations` | Enabled, `5m` interval | Aggregate Groups metrics only, with the usual lookback/window settings; no raw invocation events. (CFO-0047.01) |
| `collectors.certs.packs` | Disabled, `1h` interval | Requires SSL and Certificates Read. Snapshot collector with no checkpoint key. Publishes only after every zone read succeeds; failed reads preserve the previous snapshot until its TTL of three intervals. Successful empty reads clear both gauges. (CFO-0050.01) |
| `collectors.tunnels.status` | Disabled, `1m` interval | Requires Cloudflare Tunnel Read. Without permission the API returns an empty 200, not a 403. Snapshot collector with no checkpoint key; first poll after startup emits no status-change event. (CFO-0048.01) |

Override these keys using `CF2OTEL_OTLP__METRIC_CARDINALITY_LIMIT`,
`CF2OTEL_HTTP__REQUEST_SOURCE` and `CF2OTEL_HTTP__BREAKDOWNS`. The breakdown environment
value is comma-separated (for example `country,method`); an empty value disables all breakdowns.
Collector environment forms are `WORKERS_INVOCATIONS`, `CERTS_PACKS` and `TUNNELS_STATUS`.
Snapshot collectors use the polling interval, not the window or initial lookback.

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
| `EventTunnelStatusChange` | Log | `cloudflare.tunnel.id`, `cloudflare.tunnel.status` and observed time (record timestamp). (CFO-0048.01) |
| `EventWindowGap` | Log | `cf2otel.collector`, `cf2otel.window.from`, `cf2otel.window.floor` and `cloudflare.dns.zone` when present. |

cf2otel also emits two span families. Deduplicate the AI Gateway request span by
`cloudflare.ai_gateway.gateway.name` and `cloudflare.ai_gateway.log.id`. Deduplicate the
`cf2otel.api.request` span by its OpenTelemetry `trace_id` and `span_id`; each observed API call is a
separate span.
