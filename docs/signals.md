# Signals

All signals carry `service.name=cf2otel`. Cloudflare-specific names begin `cloudflare.*`; GenAI names follow `gen_ai.*`; the poller's own measurements begin `cf2otel.*`. Names are declared in `internal/semconv` and this page is the public inventory.

## Logs and traces

| Event or span | Source | Notes |
| --- | --- | --- |
| `cloudflare.access.login` | Access REST request log | Login action, decision, app, country and available identity details. The source has short history. |
| `cloudflare.access.scim_update` | Access SCIM update log | Resource type, HTTP method, status and available identifiers. |
| `cloudflare.http.request` | `httpRequestsAdaptive` | Sampled per-request event. Access user identity, when present, is inferred and flagged. |
| `cloudflare.ai_gateway.request` | AI Gateway REST logs | Request metadata and outcome. Request and response content is optional and capped. |
| `gen_ai.client.inference.operation.details` | AI Gateway body content | Opt-in content event, subject to the body cap. |
| GenAI client span | AI Gateway REST logs | Request model/provider, outcome, token usage and available timing. |
| `cf2otel.api.request` | Cloudflare API client | Attempt duration, method and status class; no URL or scope identifier. |

Loki stores the OTLP log attributes as structured metadata. Filter from `{service_name="cf2otel"}`, then use `| event_name="cloudflare.http.request"` or another event value. Paths, IPs, emails, user agents and ray IDs stay on logs or spans, never on metric series.

## Metrics

| Name | Meaning |
| --- | --- |
| `cloudflare.access.logins` | Human Access login count from `cf1AccessLoginsRawGroups`. |
| `cloudflare.access.requests` | Access request count from `accessLoginRequestsAdaptiveGroups`; keep `nonidentity` traffic separate. |
| `cloudflare.access.apps` | Access application inventory gauge. |
| `cloudflare.access.users` | Access user inventory gauge. |
| `cloudflare.http.requests` | Request count from sample-corrected `httpRequestsAdaptiveGroups`. |
| `cloudflare.http.origin.duration` | Average origin response duration per Groups window, in seconds. |
| `cloudflare.ai_gateway.requests` | AI Gateway request count. |
| `cloudflare.ai_gateway.errors` | AI Gateway error count. |
| `cloudflare.ai_gateway.cache_hits` | AI Gateway cache hits. |
| `cloudflare.ai_gateway.cost` | AI Gateway request cost. |
| `gen_ai.client.operation.duration` | GenAI operation duration. |
| `gen_ai.client.inference.usage.input_tokens` | Input token usage. |
| `gen_ai.client.inference.usage.output_tokens` | Output token usage. |
| `gen_ai.client.inference.usage.cache_read.input_tokens` | Cached input tokens. |
| `gen_ai.client.inference.usage.reasoning.output_tokens` | Reasoning output tokens. |
| `gen_ai.client.inference.operation.input_tokens` | Input tokens by operation. |
| `gen_ai.client.inference.operation.output_tokens` | Output tokens by operation. |
| `cf2otel.scrape.success` | Collector scrape success state. |
| `cf2otel.scrape.duration` | Collector scrape duration. |
| `cf2otel.scrape.errors` | Collector scrape errors. |
| `cf2otel.scrape.last_success_timestamp` | Time of last successful collector scrape. |
| `cf2otel.export.success` | Successful OTLP exports. |
| `cf2otel.export.errors` | Failed OTLP exports. |
| `cf2otel.build.info` | Build identity. |
| `cf2otel.checkpoint.age` | Age of the oldest collector checkpoint. |
| `cf2otel.api.requests` | Cloudflare API requests. |
| `cf2otel.api.duration` | Cloudflare API request duration. |
| `cf2otel.api.retries` | Cloudflare API retries. |
| `cf2otel.identity.matched` | HTTP events matched to one Access identity. |
| `cf2otel.identity.unmatched` | HTTP events without a match. |
| `cf2otel.identity.ambiguous` | HTTP events with more than one candidate. |

AI Gateway metrics combine Cloudflare request outcome measurements with GenAI duration and usage conventions.

## Attributes

| Group | Keys |
| --- | --- |
| Resource and common | `service.name`, `service.version`, `service.instance.id`, `event_name`, `cf2otel.collector.name`, `cf2otel.status_class`, `cf2otel.api.method` |
| Access app and decision | `cloudflare.access.app`, `cloudflare.access.app.id`, `cloudflare.access.app.type`, `cloudflare.access.connection`, `cloudflare.access.host`, `cloudflare.access.path`, `cloudflare.access.action`, `cloudflare.access.allowed`, `cloudflare.access.country`, `cloudflare.access.login_type`, `cloudflare.access.identity_provider`, `cloudflare.access.service_token` |
| Access identity | `cloudflare.access.user.email`, `cloudflare.access.user.id`, `cloudflare.access.user.ip_address`, `cloudflare.access.identity.inferred`, `cloudflare.access.identity.login_ray_id`, `cloudflare.access.ray_id` |
| Access SCIM | `cloudflare.access.scim.resource_type`, `cloudflare.access.scim.method`, `cloudflare.access.scim.status`, `cloudflare.access.scim.idp_id`, `cloudflare.access.scim.resource_id`, `cloudflare.access.scim.user_email` |
| HTTP | `cloudflare.http.host`, `cloudflare.http.method`, `cloudflare.http.path`, `cloudflare.http.query`, `cloudflare.http.status_code`, `cloudflare.http.origin_status_code`, `cloudflare.http.client_ip`, `cloudflare.http.user_agent`, `cloudflare.http.ray_id`, `cloudflare.http.zone`, `cloudflare.http.cache_status`, `cloudflare.http.security_action`, `cloudflare.http.colo` |
| Poller | `cf2otel.collector`, `cf2otel.version`, `cf2otel.commit`, `cf2otel.export.signal`, `cf2otel.build.version`, `cf2otel.build.commit` |

Only bounded attributes should be used to group metrics. `cloudflare.access.identity.inferred=true` means a time, host and IP correlation, not a Cloudflare-provided identity on the HTTP event.

## Declared name inventory

This exhaustive inventory is keyed to the `internal/semconv` declarations. It includes attributes that may appear only when the source API returns their fields or body capture is enabled.

| Kind | Name |
| --- | --- |
| Event | `cloudflare.access.login` |
| Event | `cloudflare.access.scim_update` |
| Event | `cloudflare.ai_gateway.request` |
| Event | `cloudflare.http.request` |
| Event | `gen_ai.client.inference.operation.details` |
| Metric | `cf2otel.api.duration` |
| Metric | `cf2otel.api.requests` |
| Metric | `cf2otel.api.retries` |
| Metric | `cf2otel.build.info` |
| Metric | `cf2otel.checkpoint.age` |
| Metric | `cf2otel.export.errors` |
| Metric | `cf2otel.export.success` |
| Metric | `cf2otel.identity.ambiguous` |
| Metric | `cf2otel.identity.matched` |
| Metric | `cf2otel.identity.unmatched` |
| Metric | `cf2otel.scrape.duration` |
| Metric | `cf2otel.scrape.errors` |
| Metric | `cf2otel.scrape.last_success_timestamp` |
| Metric | `cf2otel.scrape.success` |
| Metric | `cloudflare.access.apps` |
| Metric | `cloudflare.access.logins` |
| Metric | `cloudflare.access.requests` |
| Metric | `cloudflare.access.users` |
| Metric | `cloudflare.ai_gateway.cache_hits` |
| Metric | `cloudflare.ai_gateway.cost` |
| Metric | `cloudflare.ai_gateway.errors` |
| Metric | `cloudflare.ai_gateway.requests` |
| Metric | `cloudflare.http.origin.duration` |
| Metric | `cloudflare.http.requests` |
| Metric | `gen_ai.client.inference.operation.input_tokens` |
| Metric | `gen_ai.client.inference.operation.output_tokens` |
| Metric | `gen_ai.client.inference.usage.cache_read.input_tokens` |
| Metric | `gen_ai.client.inference.usage.input_tokens` |
| Metric | `gen_ai.client.inference.usage.output_tokens` |
| Metric | `gen_ai.client.inference.usage.reasoning.output_tokens` |
| Metric | `gen_ai.client.operation.duration` |
| Attribute | `cf2otel.build.commit` |
| Attribute | `cf2otel.build.version` |
| Attribute | `cf2otel.collector` |
| Attribute | `cf2otel.collector.name` |
| Attribute | `cf2otel.commit` |
| Attribute | `cf2otel.export.signal` |
| Attribute | `cf2otel.status_class` |
| Attribute | `cf2otel.version` |
| Attribute | `cloudflare.access.action` |
| Attribute | `cloudflare.access.allowed` |
| Attribute | `cloudflare.access.app` |
| Attribute | `cloudflare.access.app.id` |
| Attribute | `cloudflare.access.app.type` |
| Attribute | `cloudflare.access.connection` |
| Attribute | `cloudflare.access.country` |
| Attribute | `cloudflare.access.host` |
| Attribute | `cloudflare.access.identity.inferred` |
| Attribute | `cloudflare.access.identity.login_ray_id` |
| Attribute | `cloudflare.access.identity_provider` |
| Attribute | `cloudflare.access.login_type` |
| Attribute | `cloudflare.access.path` |
| Attribute | `cloudflare.access.ray_id` |
| Attribute | `cloudflare.access.scim.idp_id` |
| Attribute | `cloudflare.access.scim.method` |
| Attribute | `cloudflare.access.scim.resource_id` |
| Attribute | `cloudflare.access.scim.resource_type` |
| Attribute | `cloudflare.access.scim.status` |
| Attribute | `cloudflare.access.scim.user_email` |
| Attribute | `cloudflare.access.service_token` |
| Attribute | `cloudflare.access.user.email` |
| Attribute | `cloudflare.access.user.id` |
| Attribute | `cloudflare.access.user.ip_address` |
| Attribute | `cloudflare.ai_gateway.authentication.present` |
| Attribute | `cloudflare.ai_gateway.byok` |
| Attribute | `cloudflare.ai_gateway.cached` |
| Attribute | `cloudflare.ai_gateway.cost` |
| Attribute | `cloudflare.ai_gateway.created_at` |
| Attribute | `cloudflare.ai_gateway.custom_cost` |
| Attribute | `cloudflare.ai_gateway.dlp.action` |
| Attribute | `cloudflare.ai_gateway.dlp.profiles` |
| Attribute | `cloudflare.ai_gateway.duration_ms` |
| Attribute | `cloudflare.ai_gateway.event.id` |
| Attribute | `cloudflare.ai_gateway.feedback` |
| Attribute | `cloudflare.ai_gateway.gateway.name` |
| Attribute | `cloudflare.ai_gateway.guardrails` |
| Attribute | `cloudflare.ai_gateway.location.colo` |
| Attribute | `cloudflare.ai_gateway.location.region` |
| Attribute | `cloudflare.ai_gateway.log.id` |
| Attribute | `cloudflare.ai_gateway.metadata` |
| Attribute | `cloudflare.ai_gateway.model.type` |
| Attribute | `cloudflare.ai_gateway.operation` |
| Attribute | `cloudflare.ai_gateway.path` |
| Attribute | `cloudflare.ai_gateway.prompts` |
| Attribute | `cloudflare.ai_gateway.provider` |
| Attribute | `cloudflare.ai_gateway.request.body` |
| Attribute | `cloudflare.ai_gateway.request.body_truncated` |
| Attribute | `cloudflare.ai_gateway.request.content_type` |
| Attribute | `cloudflare.ai_gateway.request.head` |
| Attribute | `cloudflare.ai_gateway.request.head_complete` |
| Attribute | `cloudflare.ai_gateway.request.size` |
| Attribute | `cloudflare.ai_gateway.request.type` |
| Attribute | `cloudflare.ai_gateway.response.body` |
| Attribute | `cloudflare.ai_gateway.response.body_truncated` |
| Attribute | `cloudflare.ai_gateway.response.head` |
| Attribute | `cloudflare.ai_gateway.response.head_complete` |
| Attribute | `cloudflare.ai_gateway.response.size` |
| Attribute | `cloudflare.ai_gateway.score` |
| Attribute | `cloudflare.ai_gateway.status_code` |
| Attribute | `cloudflare.ai_gateway.step` |
| Attribute | `cloudflare.ai_gateway.success` |
| Attribute | `cloudflare.ai_gateway.timings.latency_ms` |
| Attribute | `cloudflare.ai_gateway.timings.total_ms` |
| Attribute | `cloudflare.ai_gateway.usage.total_tokens` |
| Attribute | `cloudflare.ai_gateway.user_agent` |
| Attribute | `cloudflare.ai_gateway.wholesale` |
| Attribute | `cloudflare.http.cache_status` |
| Attribute | `cloudflare.http.client_ip` |
| Attribute | `cloudflare.http.colo` |
| Attribute | `cloudflare.http.host` |
| Attribute | `cloudflare.http.method` |
| Attribute | `cloudflare.http.origin_status_code` |
| Attribute | `cloudflare.http.path` |
| Attribute | `cloudflare.http.query` |
| Attribute | `cloudflare.http.ray_id` |
| Attribute | `cloudflare.http.security_action` |
| Attribute | `cloudflare.http.status_code` |
| Attribute | `cloudflare.http.user_agent` |
| Attribute | `cloudflare.http.zone` |
| Attribute | `error.type` |
| Attribute | `event_name` |
| Attribute | `gen_ai.input.messages` |
| Attribute | `gen_ai.operation.name` |
| Attribute | `gen_ai.output.messages` |
| Attribute | `gen_ai.provider.name` |
| Attribute | `gen_ai.request.model` |
| Attribute | `gen_ai.usage.cache_read.input_tokens` |
| Attribute | `gen_ai.usage.cost` |
| Attribute | `gen_ai.usage.input_tokens` |
| Attribute | `gen_ai.usage.output_tokens` |
| Attribute | `gen_ai.usage.reasoning.output_tokens` |
| Attribute | `service.instance.id` |
| Attribute | `service.name` |
| Attribute | `service.version` |
