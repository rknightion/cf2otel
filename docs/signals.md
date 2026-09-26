# Signals

All signals carry `service.name=cf2otel`. Cloudflare-specific names begin `cloudflare.*`; GenAI names follow `gen_ai.*`; the poller's own measurements begin `cf2otel.*`. Names are declared in `internal/semconv` and this page is the public inventory.

## Logs and traces

| Event or span | Source | Notes |
| --- | --- | --- |
| `cloudflare.access.login` | Access REST request log | Login action, decision, app, country and available identity details. The source has short history. |
| `cloudflare.access.scim_update` | Access SCIM update log | Resource type, HTTP method, status and available identifiers. |
| `cloudflare.http.request` | `httpRequestsAdaptive` | Sampled per-request event. Access user identity, when present, is inferred and flagged. |
| `cloudflare.ai_gateway.request` | AI Gateway REST logs | Request metadata and outcome. Request and response content is optional and capped. |
| `gen_ai.client.inference.operation.details` | AI Gateway body content | Opt-in span event and correlated OTLP content log per available side, subject to the body cap. |
| `cloudflare.audit.event` | Account audit log v2 | Actor, action, resource and request details. Email and IP are log attributes only. |
| `cloudflare.firewall.event` | `firewallEventsAdaptive` | Per-request security action; IP, path, query, user agent and ray stay on logs. |
| `cloudflare.dns.query` | `dnsAnalyticsAdaptive` | Per-query DNS event with available query, response and network fields. Names and IPs stay on logs. |
| `cf2otel.window.gap` | Retention-gap handling | Collector, skipped window and retention floor when a source cannot backfill. |
| GenAI client span | AI Gateway REST logs | Request model/provider, outcome, token usage and available timing. |
| `cf2otel.api.request` | Cloudflare API client | Attempt duration, method and status class; no URL or scope identifier. |

Loki stores the OTLP log attributes as structured metadata. Filter from `{service_name="cf2otel"}`, then use `| event_name="cloudflare.http.request"` or another event value. Paths, IPs, emails, user agents and ray IDs stay on logs or spans, never on metric series.

## Metrics

| Name | Unit | Meaning |
| --- | --- | --- |
| `cloudflare.access.logins` | `1` | Human Access login count from `cf1AccessLoginsRawGroups`. |
| `cloudflare.access.identity_logins` | `1` | Exact REST identity-login count by app, allowed, connection and action; excludes nonidentity service-token rows. |
| `cloudflare.access.requests` | `{request}` | Access request count from `accessLoginRequestsAdaptiveGroups`; keep `nonidentity` traffic separate. |
| `cloudflare.access.apps` | `1` | Access application inventory gauge. |
| `cloudflare.access.users` | `1` | Access user inventory gauge. |
| `cloudflare.http.requests` | `{request}` | Request count from sample-corrected `httpRequestsAdaptiveGroups`. |
| `cloudflare.http.origin.duration` | `s` | Average origin response duration per Groups window, in seconds. |
| `cloudflare.audit.events` | `1` | Exact audit event count by resource product, action type and action result. |
| `cloudflare.firewall.events` | `1` | Security event count from a Groups dataset by zone. Pro Groups provides action and source dimensions; Free ByTimeGroups rejects them despite `settings.availableFields` advertising them. |
| `cloudflare.dns.queries` | `1` | DNS query count from `dnsAnalyticsAdaptiveGroups` by zone and available bounded dimensions. |
| `cloudflare.gateway.dns.queries` | `1` | Gateway DNS query sum from account-level `cf1GatewayDnsRawGroups`, by bounded query type, resolver decision and country. |
| `cloudflare.rum.page_views` | `1` | Page views from `rumPageloadEventsAdaptiveGroups` by country and device. |
| `cloudflare.rum.sessions` | `1` | Visit sum from `rumPageloadEventsAdaptiveGroups` by country and device. |
| `cloudflare.rum.lcp.p75` | `s` | Rolling p75 largest contentful paint gauge, converted from inferred GraphQL milliseconds to seconds. |
| `cloudflare.rum.inp.p75` | `s` | Rolling p75 interaction to next paint gauge, converted from inferred GraphQL milliseconds to seconds. |
| `cloudflare.rum.fid.p75` | `s` | Rolling p75 first input delay gauge, converted from inferred GraphQL milliseconds to seconds. |
| `cloudflare.rum.fcp.p75` | `s` | Rolling p75 first contentful paint gauge, converted from inferred GraphQL milliseconds to seconds. |
| `cloudflare.rum.ttfb.p75` | `s` | Rolling p75 time to first byte gauge, converted from inferred GraphQL milliseconds to seconds. |
| `cloudflare.rum.cls.p75` | `1` | Rolling p75 cumulative layout shift score gauge. |
| `cloudflare.workers.requests` | `{request}` | Worker request count from `workersOverviewRequestsAdaptiveGroups`, optionally by bounded script name. |
| `cloudflare.turnstile.events` | `1` | Turnstile event count from `turnstileAdaptiveGroups`, account aggregate. |
| `cloudflare.logpush.uploads` | `1` | Logpush upload count from `logpushHealthAdaptiveGroups`, account aggregate. |
| `cloudflare.logpush.records` | `1` | Logpush record count from `logpushHealthAdaptiveGroups`, account aggregate. |
| `cloudflare.d1.read_queries` | `1` | D1 read query sum from `d1AnalyticsAdaptiveGroups`, account aggregate. |
| `cloudflare.d1.write_queries` | `1` | D1 write query sum from `d1AnalyticsAdaptiveGroups`, account aggregate. |
| `cloudflare.d1.queries` | `1` | D1 query count from `d1QueriesAdaptiveGroups`; query text is not selected. |
| `cloudflare.d1.storage.max_database_bytes` | `By` | Maximum D1 database size across databases in the latest complete bucket, `By`. |
| `cloudflare.kv.requests` | `{request}` | KV operation request sum from `kvOperationsAdaptiveGroups`, account aggregate. |
| `cloudflare.kv.storage.max_namespace_bytes` | `By` | Maximum KV namespace bytes in the latest complete bucket, `By`. |
| `cloudflare.kv.storage.max_namespace_keys` | `{key}` | Maximum KV namespace key count in the latest complete bucket. |
| `cloudflare.r2.bandwidth.download.bytes` | `By` | R2 download byte sum, optionally by bounded bucket name, `By`. |
| `cloudflare.r2.bandwidth.upload.bytes` | `By` | R2 upload byte sum, optionally by bounded bucket name, `By`. |
| `cloudflare.r2.catalog.data.operations` | `1` | R2 catalog data operation count, optionally by bounded namespace name. |
| `cloudflare.r2.catalog.maintenance.jobs` | `1` | R2 catalog maintenance job count, optionally by bounded namespace name. |
| `cloudflare.r2.requests` | `{request}` | R2 operation request sum, optionally by bounded bucket name. |
| `cloudflare.r2.storage.payload.bytes` | `By` | R2 payload-size gauge in the latest complete bucket per bucket, `By`. |
| `cloudflare.r2.storage.objects` | `{object}` | R2 object-count gauge in the latest complete bucket per bucket. |
| `cloudflare.r2sql.queries` | `1` | R2 SQL query count, optionally by bounded bucket name; table names are omitted. |
| `cloudflare.durableobjects.requests` | `{request}` | Durable Objects invocation request sum, account aggregate. |
| `cloudflare.durableobjects.subrequests` | `{request}` | Durable Objects periodic subrequest sum, account aggregate. |
| `cloudflare.durableobjects.sql_storage.max_namespace_bytes` | `By` | Maximum Durable Objects SQL storage across namespaces in the latest complete bucket, `By`. |
| `cloudflare.durableobjects.subrequests.request_body.bytes` | `By` | Durable Objects uncached request-body byte sum, account aggregate, `By`. |
| `cloudflare.queues.backlog.max_queue_avg_messages` | `{message}` | Maximum per-queue average backlog messages in the latest complete bucket. |
| `cloudflare.queues.backlog.max_queue_avg_bytes` | `By` | Maximum per-queue average backlog bytes in the latest complete bucket, `By`. |
| `cloudflare.queues.consumer.max_queue_avg_concurrency` | `{consumer}` | Maximum per-queue average consumer concurrency in the latest complete bucket. |
| `cloudflare.queues.delayed_backlog.max_queue_avg_messages` | `{message}` | Maximum per-queue average delayed backlog messages in the latest complete bucket. |
| `cloudflare.queues.message.operations` | `1` | Queue message operation count, account aggregate. |
| `cloudflare.queues.message.billable_operations` | `1` | Queue billable operation sum, account aggregate. |
| `cloudflare.email.routing.events` | `1` | Email Routing Groups count summed across account-owned zones, account aggregate. |
| `cloudflare.email.sending.events` | `1` | Email Sending Groups count summed across account-owned zones, account aggregate. |
| `cloudflare.ai_gateway.requests` | `{request}` | AI Gateway request count. |
| `cloudflare.ai_gateway.errors` | `1` | AI Gateway error count. |
| `cloudflare.ai_gateway.cache_hits` | `1` | AI Gateway cache hits. |
| `cloudflare.ai_gateway.cost` | `1` | AI Gateway request cost. |
| `cloudflare.ai_gateway.dlp.requests` | `{request}` | AI Gateway request count with a DLP outcome (flagged, blocked or other), by gateway, action and direction; one count per distinct request/response direction, or `other` when the action carries no findings. |
| `gen_ai.client.operation.duration` | `s` | GenAI operation duration. |
| `gen_ai.client.inference.usage.input_tokens` | `{token}` | Input token usage. |
| `gen_ai.client.inference.usage.output_tokens` | `{token}` | Output token usage. |
| `gen_ai.client.inference.usage.cache_read.input_tokens` | `{token}` | Cached input tokens. |
| `gen_ai.client.inference.usage.reasoning.output_tokens` | `{token}` | Reasoning output tokens. |
| `gen_ai.client.inference.operation.input_tokens` | `{token}` | Input tokens by operation. |
| `gen_ai.client.inference.operation.output_tokens` | `{token}` | Output tokens by operation. |
| `cf2otel.scrape.success` | `1` | Collector scrape success state. |
| `cf2otel.scrape.duration` | `s` | Collector scrape duration. |
| `cf2otel.scrape.errors` | `1` | Collector scrape errors. |
| `cf2otel.scrape.last_success_timestamp` | `s` | Time of last successful collector scrape. |
| `cf2otel.export.success` | `1` | Successful OTLP exports. |
| `cf2otel.export.errors` | `1` | Failed OTLP exports. |
| `cf2otel.build.info` | `1` | Build identity. |
| `cf2otel.checkpoint.age` | `s` | Age of the oldest collector checkpoint. |
| `cf2otel.api.requests` | `{request}` | Cloudflare API requests. |
| `cf2otel.api.duration` | `s` | Cloudflare API request duration. |
| `cf2otel.api.retries` | `1` | Cloudflare API retries. |
| `cf2otel.identity.matched` | `1` | HTTP events matched to one Access identity. |
| `cf2otel.identity.unmatched` | `1` | HTTP events without a match. |
| `cf2otel.identity.ambiguous` | `1` | HTTP events with more than one candidate. |
| `cf2otel.window.gap` | `s` | Skipped retention-gap seconds by collector. |
| `cf2otel.window.commit_failures` | `1` | Failed window commits by retry or dropped outcome. |
| `cf2otel.window.catchup_windows` | `1` | Additional bounded collector windows committed in one scheduler tick. |

Platform gauges select the latest complete five-minute source bucket and emit at export time. The emitter attaches the listed UCUM units to OTLP instruments. RUM timing values are converted from source milliseconds to seconds before recording. The GenAI operation histogram and poller API/scrape histograms use explicit second-based buckets, including sub-second boundaries. Queue IDs are used only inside source aggregation and are never metric attributes.

Prometheus compatibility naming adds `_seconds` for `s` when the base name lacks it and `_ratio` for dimensionless gauges. Names already ending in `_bytes` retain that suffix, and annotated `{token}` and `{request}` units add no suffix. The generated Grafana queries accept both old and new series names during rollout and revert.

AI Gateway metrics combine Cloudflare request outcome measurements with GenAI duration and usage conventions.

The retired AI Gateway dashboard's **Data boundaries** panel was static provenance guidance; **Gateway metadata exceptions** was a row grouping failed-request and DLP tables, not a separate API field. The AI Gateway Logs API inventory sampled on 2026-09-26 found no data-boundary or exception fields in 50 log-detail rows. Of that sample, 24 rows had a non-null `dlp_action` (`FLAG`), each with one `dlp_profiles` finding checking either the request (23) or the response (1); `dlp_action` maps to `flagged` (`FLAG`), `blocked` (`BLOCK`) or `other` (any other non-empty value), and a request with an action but no findings still counts once on `cloudflare.ai_gateway.dlp.requests` with direction `other`.

## Attributes

| Group | Keys |
| --- | --- |
| Resource and common | `service.name`, `service.version`, `service.instance.id`, `event_name`, `cf2otel.collector.name`, `cf2otel.status_class`, `cf2otel.api.method` |
| Access app and decision | `cloudflare.access.app`, `cloudflare.access.app.id`, `cloudflare.access.app.type`, `cloudflare.access.connection`, `cloudflare.access.host`, `cloudflare.access.path`, `cloudflare.access.action`, `cloudflare.access.allowed`, `cloudflare.access.country`, `cloudflare.access.login_type`, `cloudflare.access.identity_provider`, `cloudflare.access.service_token` |
| Access identity | `cloudflare.access.user.email`, `cloudflare.access.user.id`, `cloudflare.access.user.ip_address`, `cloudflare.access.identity.inferred`, `cloudflare.access.identity.login_ray_id`, `cloudflare.access.ray_id` |
| Access SCIM | `cloudflare.access.scim.resource_type`, `cloudflare.access.scim.method`, `cloudflare.access.scim.status`, `cloudflare.access.scim.idp_id`, `cloudflare.access.scim.resource_id`, `cloudflare.access.scim.user_email` |
| HTTP | `cloudflare.http.host`, `cloudflare.http.method`, `cloudflare.http.path`, `cloudflare.http.query`, `cloudflare.http.status_code`, `cloudflare.http.origin_status_code`, `cloudflare.http.client_ip`, `cloudflare.http.user_agent`, `cloudflare.http.ray_id`, `cloudflare.http.zone`, `cloudflare.http.cache_status`, `cloudflare.http.security_action`, `cloudflare.http.colo` |
| AI Gateway content | `cloudflare.ai_gateway.content.side`, `cloudflare.ai_gateway.content.length` |
| AI Gateway DLP | `cloudflare.ai_gateway.dlp.action`, `cloudflare.ai_gateway.dlp.direction` are also bounded metric dimensions on `cloudflare.ai_gateway.dlp.requests`; `cloudflare.ai_gateway.dlp.policy.id` and `cloudflare.ai_gateway.dlp.profile.id` are log only. |
| Audit | `cloudflare.audit.*` attributes are listed individually below; actor email and IP are log only. |
| Firewall | `cloudflare.firewall.*` attributes are listed individually below; IP, path, query, user agent and ray are log only. |
| DNS | `cloudflare.dns.*` attributes are listed individually below; query name and IPs are log only. |
| Gateway DNS | `cloudflare.gateway.dns.query.type`, `cloudflare.gateway.dns.decision`, `cloudflare.gateway.dns.country`; only bounded metric dimensions. |
| Platform resource names | `cloudflare.workers.script_name`, `cloudflare.r2.bucket_name`, `cloudflare.r2.catalog.namespace_name`, `cloudflare.r2sql.bucket_name`; bounded names only. |
| RUM | `cloudflare.rum.country`, `cloudflare.rum.device_type`, `cloudflare.rum.site_tag`; gauges use device and optional site tag. |
| Window delivery | `cf2otel.window.*` describes retention gaps and commit outcomes. |
| Poller | `cf2otel.collector`, `cf2otel.version`, `cf2otel.commit`, `cf2otel.export.signal`, `cf2otel.build.version`, `cf2otel.build.commit` |

Only bounded attributes should be used to group metrics. `cloudflare.access.identity.inferred=true` means a time, host and IP correlation, not a Cloudflare-provided identity on the HTTP event.

## Declared name inventory

This exhaustive inventory is keyed to the `internal/semconv` declarations. It includes attributes that may appear only when the source API returns their fields or body capture is enabled.

| Kind | Name |
| --- | --- |
| Event | `cf2otel.window.gap` |
| Event | `cloudflare.access.login` |
| Event | `cloudflare.access.scim_update` |
| Event | `cloudflare.ai_gateway.request` |
| Event | `cloudflare.audit.event` |
| Event | `cloudflare.dns.query` |
| Event | `cloudflare.firewall.event` |
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
| Metric | `cf2otel.window.commit_failures` |
| Metric | `cf2otel.window.catchup_windows` |
| Metric | `cf2otel.window.gap` |
| Metric | `cloudflare.access.apps` |
| Metric | `cloudflare.access.identity_logins` |
| Metric | `cloudflare.access.logins` |
| Metric | `cloudflare.access.requests` |
| Metric | `cloudflare.access.users` |
| Metric | `cloudflare.ai_gateway.cache_hits` |
| Metric | `cloudflare.ai_gateway.cost` |
| Metric | `cloudflare.ai_gateway.dlp.requests` |
| Metric | `cloudflare.ai_gateway.errors` |
| Metric | `cloudflare.ai_gateway.requests` |
| Metric | `cloudflare.audit.events` |
| Metric | `cloudflare.dns.queries` |
| Metric | `cloudflare.email.routing.events` |
| Metric | `cloudflare.email.sending.events` |
| Metric | `cloudflare.gateway.dns.queries` |
| Metric | `cloudflare.firewall.events` |
| Metric | `cloudflare.http.origin.duration` |
| Metric | `cloudflare.http.requests` |
| Metric | `cloudflare.rum.page_views` |
| Metric | `cloudflare.rum.sessions` |
| Metric | `cloudflare.rum.lcp.p75` |
| Metric | `cloudflare.rum.inp.p75` |
| Metric | `cloudflare.rum.fid.p75` |
| Metric | `cloudflare.rum.fcp.p75` |
| Metric | `cloudflare.rum.ttfb.p75` |
| Metric | `cloudflare.rum.cls.p75` |
| Metric | `gen_ai.client.inference.operation.input_tokens` |
| Metric | `gen_ai.client.inference.operation.output_tokens` |
| Metric | `gen_ai.client.inference.usage.cache_read.input_tokens` |
| Metric | `gen_ai.client.inference.usage.input_tokens` |
| Metric | `gen_ai.client.inference.usage.output_tokens` |
| Metric | `gen_ai.client.inference.usage.reasoning.output_tokens` |
| Metric | `gen_ai.client.operation.duration` |
| Metric | `cloudflare.d1.queries` |
| Metric | `cloudflare.d1.read_queries` |
| Metric | `cloudflare.d1.storage.max_database_bytes` |
| Metric | `cloudflare.d1.write_queries` |
| Metric | `cloudflare.durableobjects.requests` |
| Metric | `cloudflare.durableobjects.sql_storage.max_namespace_bytes` |
| Metric | `cloudflare.durableobjects.subrequests` |
| Metric | `cloudflare.durableobjects.subrequests.request_body.bytes` |
| Metric | `cloudflare.kv.requests` |
| Metric | `cloudflare.kv.storage.max_namespace_bytes` |
| Metric | `cloudflare.kv.storage.max_namespace_keys` |
| Metric | `cloudflare.logpush.records` |
| Metric | `cloudflare.logpush.uploads` |
| Metric | `cloudflare.queues.backlog.max_queue_avg_bytes` |
| Metric | `cloudflare.queues.backlog.max_queue_avg_messages` |
| Metric | `cloudflare.queues.consumer.max_queue_avg_concurrency` |
| Metric | `cloudflare.queues.delayed_backlog.max_queue_avg_messages` |
| Metric | `cloudflare.queues.message.billable_operations` |
| Metric | `cloudflare.queues.message.operations` |
| Metric | `cloudflare.r2.bandwidth.download.bytes` |
| Metric | `cloudflare.r2.bandwidth.upload.bytes` |
| Metric | `cloudflare.r2.catalog.data.operations` |
| Metric | `cloudflare.r2.catalog.maintenance.jobs` |
| Metric | `cloudflare.r2.requests` |
| Metric | `cloudflare.r2.storage.objects` |
| Metric | `cloudflare.r2.storage.payload.bytes` |
| Metric | `cloudflare.r2sql.queries` |
| Metric | `cloudflare.turnstile.events` |
| Metric | `cloudflare.workers.requests` |
| Attribute | `cloudflare.r2.bucket_name` |
| Attribute | `cloudflare.r2.catalog.namespace_name` |
| Attribute | `cloudflare.r2sql.bucket_name` |
| Attribute | `cloudflare.workers.script_name` |
| Attribute | `cf2otel.api.method` |
| Attribute | `cf2otel.build.commit` |
| Attribute | `cf2otel.build.version` |
| Attribute | `cf2otel.collector` |
| Attribute | `cf2otel.collector.name` |
| Attribute | `cf2otel.commit` |
| Attribute | `cf2otel.export.signal` |
| Attribute | `cf2otel.status_class` |
| Attribute | `cf2otel.version` |
| Attribute | `cf2otel.window.floor` |
| Attribute | `cf2otel.window.from` |
| Attribute | `cf2otel.window.gap_seconds` |
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
| Attribute | `cloudflare.ai_gateway.content.length` |
| Attribute | `cloudflare.ai_gateway.content.side` |
| Attribute | `cloudflare.ai_gateway.cost` |
| Attribute | `cloudflare.ai_gateway.created_at` |
| Attribute | `cloudflare.ai_gateway.custom_cost` |
| Attribute | `cloudflare.ai_gateway.dlp.action` |
| Attribute | `cloudflare.ai_gateway.dlp.direction` |
| Attribute | `cloudflare.ai_gateway.dlp.policy.id` |
| Attribute | `cloudflare.ai_gateway.dlp.profile.id` |
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
| Attribute | `cloudflare.ai_gateway.request.body_unavailable` |
| Attribute | `cloudflare.ai_gateway.request.content_type` |
| Attribute | `cloudflare.ai_gateway.request.head` |
| Attribute | `cloudflare.ai_gateway.request.head_complete` |
| Attribute | `cloudflare.ai_gateway.request.size` |
| Attribute | `cloudflare.ai_gateway.request.type` |
| Attribute | `cloudflare.ai_gateway.response.body` |
| Attribute | `cloudflare.ai_gateway.response.body_truncated` |
| Attribute | `cloudflare.ai_gateway.response.body_unavailable` |
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
| Attribute | `cloudflare.audit.action.description` |
| Attribute | `cloudflare.audit.action.result` |
| Attribute | `cloudflare.audit.action.time` |
| Attribute | `cloudflare.audit.action.type` |
| Attribute | `cloudflare.audit.actor.email` |
| Attribute | `cloudflare.audit.actor.id` |
| Attribute | `cloudflare.audit.actor.ip` |
| Attribute | `cloudflare.audit.actor.token.id` |
| Attribute | `cloudflare.audit.actor.token.name` |
| Attribute | `cloudflare.audit.actor.type` |
| Attribute | `cloudflare.audit.id` |
| Attribute | `cloudflare.audit.raw.method` |
| Attribute | `cloudflare.audit.raw.ray_id` |
| Attribute | `cloudflare.audit.raw.status_code` |
| Attribute | `cloudflare.audit.raw.uri` |
| Attribute | `cloudflare.audit.raw.user_agent` |
| Attribute | `cloudflare.audit.resource.id` |
| Attribute | `cloudflare.audit.resource.product` |
| Attribute | `cloudflare.audit.resource.type` |
| Attribute | `cloudflare.dns.zone` |
| Attribute | `cloudflare.dns.query.name` |
| Attribute | `cloudflare.dns.query.type` |
| Attribute | `cloudflare.dns.response.code` |
| Attribute | `cloudflare.dns.response.cached` |
| Attribute | `cloudflare.dns.response.stale` |
| Attribute | `cloudflare.dns.protocol` |
| Attribute | `cloudflare.dns.colo` |
| Attribute | `cloudflare.dns.source.ip` |
| Attribute | `cloudflare.dns.destination.ip` |
| Attribute | `cloudflare.dns.upstream.ip` |
| Attribute | `cloudflare.dns.ip.version` |
| Attribute | `cloudflare.dns.sample.interval` |
| Attribute | `cloudflare.dns.query.size` |
| Attribute | `cloudflare.dns.response.size` |
| Attribute | `cloudflare.firewall.action` |
| Attribute | `cloudflare.firewall.client.asn` |
| Attribute | `cloudflare.firewall.client.asn_description` |
| Attribute | `cloudflare.firewall.client.country` |
| Attribute | `cloudflare.firewall.client.ip` |
| Attribute | `cloudflare.firewall.colo` |
| Attribute | `cloudflare.firewall.edge_status_code` |
| Attribute | `cloudflare.firewall.host` |
| Attribute | `cloudflare.firewall.kind` |
| Attribute | `cloudflare.firewall.method` |
| Attribute | `cloudflare.firewall.origin_status_code` |
| Attribute | `cloudflare.firewall.path` |
| Attribute | `cloudflare.firewall.protocol` |
| Attribute | `cloudflare.firewall.query` |
| Attribute | `cloudflare.firewall.ray_id` |
| Attribute | `cloudflare.firewall.rule_id` |
| Attribute | `cloudflare.firewall.ruleset_id` |
| Attribute | `cloudflare.firewall.source` |
| Attribute | `cloudflare.firewall.user_agent` |
| Attribute | `cloudflare.firewall.waf_attack_score_class` |
| Attribute | `cloudflare.firewall.zone` |
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
| Attribute | `cloudflare.gateway.dns.query.type` |
| Attribute | `cloudflare.gateway.dns.decision` |
| Attribute | `cloudflare.gateway.dns.country` |
| Attribute | `cloudflare.rum.site_tag` |
| Attribute | `cloudflare.rum.device_type` |
| Attribute | `cloudflare.rum.country` |
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
| Attribute | `outcome` |
| Attribute | `service.instance.id` |
| Attribute | `service.name` |
| Attribute | `service.version` |
