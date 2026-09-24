# Configuration

Settings load in this order: built-in defaults, YAML, then `CF2OTEL_` environment variables. Use `__` between nested keys. The [generated environment variable reference](env-vars.md) lists the struct-backed keys.

| Section | Important settings |
| --- | --- |
| `cloudflare` | `account_id`, optional `zones`, API base, timeout and response limit. `api_token` is environment-only. |
| `collectors` | `enabled`, `interval`, `initial_lookback` and `max_window` per named collector. |
| `access` | `include_service_tokens` keeps service-token activity separate from human logins. |
| `http` | `scope` controls request events and defaults to `access_protected`. `metrics_scope` inherits it by default; set `metrics_scope: all` to collect Groups metrics across account zones without widening events. `hosts` is required for either `hosts` scope. `max_metric_hosts_per_zone` (1000) and `max_metric_series_per_window` (10000) reject oversized metric windows before checkpoint advance. |
| `identity` | Enable the inferred login-to-request join and bound its `match_window` and `max_candidates`. |
| `ai_gateway` | Select `gateways`, opt in to `capture_bodies`, cap each body with `max_body_bytes`, and link caller traces when headers allow. |
| `otlp` | `endpoint`, `protocol` (`http` or `grpc`), Grafana Cloud instance ID and environment-only token or headers. |
| `state` | Persistent checkpoint directory; default `/var/lib/cf2otel`. |
| `health`, `log` | Loopback health listener and application logging. |

Collector keys are `access.logins`, `access.login_metrics`, `access.scim`, `inventory.access`, `httpreq.events`, `httpreq.metrics`, `aigateway.logs`, `aigateway.metrics`, `audit.logs`, `firewall.events`, `firewall.metrics`, `dns.events`, `dns.metrics`, `rum.pageloads`, `rum.web_vitals`, `gateway.dns`, and `selfobs`. Enabled collectors default to five-minute intervals. `aigateway.metrics` is disabled and unscheduled because its GraphQL Groups ingestion lag is not bounded; `aigateway.logs` emits the AI Gateway metrics from REST rows. The default initial lookback is 30 minutes and maximum window is one hour. The scheduler advances a checkpoint only after a successful window.

The Access REST log has only about a day's observed reach. Keep its polling interval short and preserve the state directory; a long outage cannot be repaired by expanding the lookback. Cloudflare GraphQL retention and permitted window width vary by dataset and plan, so cf2otel negotiates available fields and splits requests to fit reported limits.

`httpRequestsAdaptive` event rows are sampled. Use the companion `httpreq.metrics` collector for corrected aggregate counts; do not count event rows to calculate a request rate.

See [Security and PII](security.md) before enabling AI Gateway body capture or wider HTTP scope.
