# Signals

All signals carry `service.name=cf2otel`. Cloudflare-specific names begin `cloudflare.*`; GenAI names follow `gen_ai.*`; the poller's own measurements begin `cf2otel.*`. Names are declared in `internal/semconv` and this page is the public inventory.

## Source deny-lists

The [source deny-list settings](configuration.md#source-metric-and-attribute-deny-lists)
use exact semantic names from this inventory before Prometheus translation.
`otlp.metric_denylist` suppresses SDK registration/observation; `otlp.attribute_denylist`
removes keys from every signal attribute bag, including generated `event_name`
and nested span bags, but not resources or bodies. Both default to empty. Denying
`event_name` removes the usual event-name log query selector, not the log signal.
Dropping dimensions merges counter/histogram series and loses their distinction;
gauge and retained-snapshot collisions use the last input point. Denying metrics
does not disable their collectors or upstream reads.

## Zone selection and poll gauges

`zones.exclude` (or comma-separated `CF2OTEL_ZONES__EXCLUDE`) defaults to empty.
Selectors match exact zone IDs or case-insensitive zone names, with no regex or
wildcard expansion. Each zone consumer first applies its existing include and
account-ownership rules and validates them, then removes excluded zones. An
unmatched exclusion is ignored; an unmatched include still has its existing
behavior. A valid selection entirely removed by exclusions is a successful empty
zone poll. Account-level queries, including Logpush's account scope, are unchanged.

The four `cf2otel.zones.*` gauges use only `cf2otel.collector`; filtered and skipped
also use `cf2otel.zone.reason`. Reasons are bounded to `include`, `exclude`,
`unentitled`, `no_account`, and `other`. Discovered counts the API discovery result;
filtered counts zones removed by selection; processed counts distinct zones
actually queried, not settings reads, query subdivisions, or datasets; skipped
counts remaining eligible zones not queried. No zone name or ID is a gauge label.
Every successful complete poll emits zero for absent reason series. Failed polls
retain the existing no-partial-output behavior and do not refresh these gauges.

## WARP recently seen fleet

`cloudflare.warp.devices` is a Gauge with unit `1`, from account-scoped
`GET /accounts/{account}/dex/fleet-status/devices`, source `last_seen`. It counts
unique devices within `warp.last_seen_window` (default 15 minutes, maximum 60),
not the total physical or licensed fleet. Network status strings are preserved
as observed; there is no documented connected/active Boolean classification.

Its five string dimensions are `cloudflare.warp.status`,
`cloudflare.warp.platform`, `cloudflare.warp.client_version`,
`cloudflare.warp.mode`, and `cloudflare.warp.colo`. A sixth string attribute,
`cloudflare.warp.remainder`, is `false` on normal tuples and `true` on overflow.
Normal tuples are sorted lexically, retaining the first `warp.max_metric_series`
(default 500); excess tuples and tuples containing a dimension longer than 256
bytes are summed into one all-`other` remainder. This preserves the device total
and distinguishes a real all-`other` tuple from overflow. Empty dimension strings
remain observed empty strings. No device ID, user identity, timestamp, raw body,
log, trace or additional metric is emitted. IDs exist only during a poll for
identical-tuple deduplication; conflicting duplicate IDs fail the whole snapshot.

Pages are fetched in ascending order, respecting a server page size at most 50,
until a short/empty page (including an empty read after a full final page).
Reported totals are not completion proof. Invalid/missing/null required strings,
malformed arrays, HTTP failures, mismatched paging, stalled full pages or the
10,000-page/100,000-unique-device safety bounds fail without partial publication
or expiry refresh. Successful empty arrays clear previous points; successful
snapshots expire after three actual configured collector intervals.

This opt-in collector uses the existing expiring SDK snapshot batch emitter,
never ordinary cumulative Gauge fallback. The row shape is **doc-derived** from
the [official device method](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/fleet_status/subresources/devices/methods/list/)
and pinned by local fixtures. The root's current canary observed only successful
empty rows (`doc-0003`, section 16); populated rows and real fleet pagination are
not live-verified. Dashboard delivery is a separate pending work item.

## DEX test results

The opt-in `dex.tests` snapshot collector emits the following Gauges from provider
`.avg` values. It does not recompute slot statistics or query individual hops.

| Metric | Unit | Provider field |
| --- | --- | --- |
| `cloudflare.dex.http.fetch_time` | `ms` | `httpStats.resourceFetchTimeMs.avg` |
| `cloudflare.dex.traceroute.rtt` | `ms` | `tracerouteStats.roundTripTimeMs.avg` |
| `cloudflare.dex.traceroute.hops` | `{hop}` | `tracerouteStats.hopsCount.avg`; the average can be fractional |
| `cloudflare.dex.packet_loss` | `%` | `tracerouteStats.packetLossPct.avg`; percentage, not a fraction |
| `cloudflare.dex.availability` | `%` | `httpStats.availabilityPct.avg` or `tracerouteStats.availabilityPct.avg` |

All points have only string attributes `cloudflare.dex.test.name` and
`cloudflare.dex.test.kind` (`http` or `traceroute`). No account, device or test ID,
URL, address, raw body, log or trace is exported. A named test must start with an
ASCII letter and contain only ASCII letters, digits, spaces, underscores and
hyphens, at most 128 characters; reserved `other` and other names go to the
remainder. Operators must still avoid sensitive names. Duplicate name/kind pairs
with distinct IDs fail catalog enumeration instead of merging or labeling by ID.

`dex.max_metric_series` bounds sticky named identities plus six reserved
signal/kind remainders across polls until restart. Overflow points have test name
`other` and preserve test kind. Their value is the arithmetic mean of the
available per-test provider averages for that signal/kind, not a weighted
fleet statistic. Missing/null optional averages are omitted, not fabricated as
zero; finite nonnegative values are required, with percentages at most 100.
Invalid results fail only that test. See [configuration](configuration.md#dex-test-result-snapshots)
for cap allocation, failure isolation and expiry semantics.

The catalog is `GET /accounts/{account}/dex/tests/overview`, with `tests[]`
containing `id`, `name` and `kind`, fully enumerated at 50 rows per page. Detail
reads use `GET /accounts/{account}/dex/http-tests/{test}` or
`GET /accounts/{account}/dex/traceroute-tests/{test}`, with UTC millisecond ISO
`from`/`to` and `interval=minute`. The default result window is one hour, bounded
to seven days, with no detail pagination. Provider averages keep their documented
units exactly. Shapes come from the official [HTTP test result method](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/http_tests/methods/get/)
and [traceroute result method](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/traceroute_tests/methods/get/),
and are proved locally with fixtures, a real shared HTTP client and the SDK.
The drift contract registers all three paths but reports both detail templates
as `documented_only, unprobed`; no live tests exist. Only the list endpoint canary
is live-probed. Populated/live-runtime proof and dashboard panels remain separate
root-owned work; this source does not claim full task acceptance.

## Workers AI aggregate metrics

The opt-in `workersai.metrics` collector reads account-scoped
`aiInferenceAdaptiveGroups`, not raw inference rows. It emits additive Counters
from complete UTC five-minute half-open buckets, with no metric attributes:

| Metric | Unit | Source |
| --- | --- | --- |
| `cloudflare.workers_ai.inferences` | `1` | `count`, total number of inferences for an account |
| `cloudflare.workers_ai.input_tokens` | `{token}` | `sum.totalInputTokens` |
| `cloudflare.workers_ai.output_tokens` | `{token}` | `sum.totalOutputTokens` |
| `cloudflare.workers_ai.inference_time` | `s` | `sum.totalInferenceTimeMs` divided by 1000 |

Count and bucket time are required; optional sums are selected only when
advertised and within the field limit. Missing/null optional values are omitted,
not zero-filled. Empty windows emit no fabricated metrics. Counts and sums add
across successful windows; a malformed value or saturated single bucket fails
the whole window without checkpoint advancement or partial publication. Query
limits and dataset duration/retention are respected, with saturated windows
bisected at five-minute boundaries. The existing platform series cap applies.
No model/resource ID, tag, cost, byte metric, log or trace is selected or emitted.

The scheduler holds back ten minutes and aligns its upper bound to a complete
five-minute bucket. This is operational policy, not a guarantee of ingestion
latency or recovery of late arrivals after a checkpoint has advanced. Dashboard
panels and live canary/proof delivery are separate root-owned work.

## Logs and traces

| Event or span | Source | Notes |
| --- | --- | --- |
| `cloudflare.tunnel.status_change` | Tunnel REST snapshot | ID, name, status and previous status; first poll emits none. (CFO-0048.01) |
| `cloudflare.access.login` | Access REST request log | Login action, decision (`cloudflare.access.allowed`: `true`/`false`), app, uppercase ISO 3166-1 alpha-2 country and available identity details. The source has short history. |
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

## Extension declarations (collector implementations pending)

These declarations reserve the following signals; this seam-only change does not emit them. Existing outputs remain unchanged. Every selection uses advertised `settings.availableFields` names in `part_field` form and respects field budgets, `maxDuration` and `notOlderThan`. Health checks are disabled on Free and enabled on the verified Pro plan; disabled scopes are skipped, not queried. HTTP visits are available on Free/Pro adaptive Groups. Threat rollups have observed retention of about three days on Free and seven days on Pro, with a three-day maximum query duration on both. Account adaptive HTTP settings permit 32 days, sufficient for a 31-day UTC month. Zone Logpush settings permit one day on Free and seven on Pro (retention includes about one extra hour); account Logpush permits about 30 days. Account DO/D1/Queue settings permit 32-day windows and 90-day retention. These are observed plan settings, not universal guarantees; runtime settings remain authoritative. No new permission or resource is provisioned.

| Name | Kind / unit | Meaning |
| --- | --- | --- |
| `cloudflare.logpush.failed_uploads` | Counter / `{upload}` | Upload failures by scope, zone name for zone scope, job ID, destination type, status code and final attempt. Source success/final are uint8 flags, job ID is uint64; success=0 means failure, final=1 with status>=300 means final loss. Explicit opt-in and platform cap; old uploads/records unchanged. (CFO-0047.03) |
| `cloudflare.http.visits` | Counter / `{visit}` | Additive `sum.visits`, zone only, inside existing adaptive Groups metrics with configured request-source policy. (CFO-0046.05) |
| `cloudflare.http.threats` | Counter / `{request}` | `sum.threats` from complete UTC-hour rollups, held back at least ten minutes; zone only, not assumed eyeball-filterable. (CFO-0046.05) |
| `cloudflare.http.account.transfer.month_to_date` | Gauge / `By` | UTC month-to-date eyeball response bytes through latest held-back complete five-minute end; no account ID/host. Snapshot without checkpoint. Successful empty results mean zero; errors/null do not. (CFO-0046.05) |
| `cloudflare.http.account.transfer.projected_month_total` | Gauge / `By` | MTD bytes * exact UTC month seconds / elapsed complete-period seconds; zero when no complete elapsed period, including month rollover. Always eyeball-only. (CFO-0046.05) |
| `cloudflare.durableobjects.errors` | Counter / `{error}` | Invocation error sum by bounded Workers script name; old request totals preserved. (CFO-0047.02) |
| `cloudflare.durableobjects.wall_time` | Gauge / `s` | Latest complete five-minute script bucket wall-time p50/p75/p99/p999, microseconds to seconds; never combine status-group quantiles. (CFO-0047.02) |
| `cloudflare.durableobjects.response_size` | Gauge / `By` | Latest complete five-minute script bucket responseBodySize p50/p75/p99/p999, bytes. No namespace/object IDs. (CFO-0047.02) |
| `cloudflare.d1.rows_read` | Counter / `{row}` | Account aggregate rows read, no database ID. (CFO-0047.02) |
| `cloudflare.d1.rows_written` | Counter / `{row}` | Account aggregate rows written, no database ID. (CFO-0047.02) |
| `cloudflare.d1.query.batch_time` | Gauge / `s` | Latest complete account bucket queryBatchTimeMs p50/p75/p99/p999, milliseconds to seconds. (CFO-0047.02) |
| `cloudflare.d1.query.batch_response_size` | Gauge / `By` | Latest complete account bucket queryBatchResponseBytes p50/p75/p99/p999. No query text/identifier. (CFO-0047.02) |
| `cloudflare.queues.message.max_queue_avg_lag` | Gauge / `s` | Latest observed complete bucket maximum of per-queue average lag, ReadMessage only; milliseconds to seconds, no queue ID. Not a universal snapshot-retirement policy. (CFO-0047.02) |
| `cloudflare.queues.message.max_queue_avg_retries` | Gauge / `{retry}` | Latest observed complete bucket maximum of per-queue average retries, ReadMessage only; no queue ID. Missing/negative N/A values omitted, real zero retained. (CFO-0047.02) |
| `cloudflare.queues.message.billable_operations.by_action` | Counter / `{operation}` | `sum.billableOperations` by action type, consumer type and outcome; no queue ID, old aggregate unchanged. (CFO-0047.02) |
| `cloudflare.health_check.events` | Counter / `{event}` | Pro-only opt-in event count by zone name, source healthStatus string and failure reason; separate from timing grouping. (CFO-0050.03) |
| `cloudflare.health_check.rtt` | Gauge / `s` | Latest complete per-origin bucket average rttMs / 1000, bounded non-IP identity; omit absent/N/A, no fabricated zero. (CFO-0050.03) |
| `cloudflare.health_check.ttfb` | Gauge / `s` | Latest complete per-origin bucket average timeToFirstByteMs / 1000. (CFO-0050.03) |
| `cloudflare.health_check.tcp_connection` | Gauge / `s` | Latest complete per-origin bucket average tcpConnMs / 1000. (CFO-0050.03) |
| `cloudflare.health_check.tls_handshake` | Gauge / `s` | Latest complete per-origin bucket average tlsHandshakeMs / 1000. (CFO-0050.03) |

| Attribute | Meaning |
| --- | --- |
| `cloudflare.logpush.scope` | `account` or `zone`. (CFO-0047.03) |
| `cloudflare.logpush.zone` | Zone name, never account/zone ID. (CFO-0047.03) |
| `cloudflare.logpush.job_id` | Source numeric job ID; explicit opt-in and platform cap. (CFO-0047.03) |
| `cloudflare.logpush.destination_type` | Bounded source destination type. (CFO-0047.03) |
| `cloudflare.logpush.status_code` | Source destination status code as string. (CFO-0047.03) |
| `cloudflare.logpush.final_attempt` | `true`/`false` from source final 0/1. (CFO-0047.03) |
| `cloudflare.queues.action_type` | Source action type. (CFO-0047.02) |
| `cloudflare.queues.consumer_type` | Source consumer type. (CFO-0047.02) |
| `cloudflare.queues.outcome` | Source outcome. (CFO-0047.02) |
| `cloudflare.health_check.zone` | Zone name. (CFO-0050.03) |
| `cloudflare.health_check.status` | Source string healthStatus, not an invented boolean mapping. (CFO-0050.03) |
| `cloudflare.health_check.failure_reason` | Bounded source reason; empty maps to `none`. (CFO-0050.03) |
| `cloudflare.health_check.origin` | Non-IP FQDN or human health-check name, bounded to 128 characters; no originIP/raw resource ID, omit timings without usable identity. (CFO-0050.03) |
| `cloudflare.workers.script_name` (reused by DO) | Bounded script name; omit new per-script points when unavailable. (CFO-0047.02) |
| `cloudflare.statistic` (reused by DO/D1) | p50/p75/p99/p999 for extension quantiles; old statistic values unchanged. (CFO-0047.02) |

## Metrics

The seat source is `GET /accounts/{account}/access/users`. Cloudflare's [official Go SDK `AccessUserListResponse`](https://github.com/cloudflare/cloudflare-go/blob/05ca1e4fc7c7f02ffd8257ea199c985e1ec618e8/zero_trust/accessuser.go#L347-L368) defines `access_seat` and `gateway_seat` as boolean flags, separately from active device count. cf2otel preserves field presence: false is valid, but missing, null and nonboolean flags fail the scrape. Each successful full-list scrape emits exactly two gauge points, including two zeros for a genuinely empty list. A user with both flags contributes one to each type. Existing `cloudflare.access.users` inventory is unchanged.

| Name | Unit | Meaning |
| --- | --- | --- |
| `cloudflare.http.response.bytes` | `By` | Response byte count on the HTTP request dimensions. (CFO-0046.01) |
| `cloudflare.http.edge.ttfb` | `s` | Edge time to first byte by zone, host and statistic (avg, p50, p95, p99); Pro only. (CFO-0046.04) |
| `cloudflare.http.origin.response_time` | `s` | Origin response time by zone, host and statistic (p50, p95, p99). (CFO-0046.04) |
| `cf2otel.http.latency.host_variants` | Counter / `{group}` | Discarded latency source groups whose raw hosts normalize to the same host, zone only. Timings come from the largest-count group; equal counts prefer the raw host equal to the normalized host (then lexical raw-host order). Percentiles are never summed or averaged. Request, byte and breakdown totals remain additive across all groups. A latency query/validation failure fails the buffered window explicitly with its checkpoint unchanged, preserving all totals for retry. |
| `cloudflare.http.requests.by_status` | `{request}` | Request count by enabled edge and origin status. Zone-level, without host. (CFO-0046.02) |
| `cloudflare.http.requests.by_country` | `{request}` | Request count by client country. Zone-level, without host. (CFO-0046.02) |
| `cloudflare.http.requests.by_protocol` | `{request}` | Request count by enabled HTTP and TLS protocol. Zone-level, without host. (CFO-0046.02) |
| `cloudflare.http.requests.by_method` | `{request}` | Request count by HTTP method. Zone-level, without host. (CFO-0046.02) |
| `cloudflare.http.requests.by_content_type` | `{request}` | Request count by response content type. Zone-level, without host. (CFO-0046.02) |
| `cloudflare.http.response.bytes.by_country` | `By` | Response byte count by zone and client country, without host. (CFO-0046.02) |
| `cloudflare.workers.invocations` | `{request}` | Invocation count by script name and status. (CFO-0047.01) |
| `cloudflare.workers.errors` | `{error}` | Invocation error count by script name. (CFO-0047.01) |
| `cloudflare.workers.subrequests` | `{request}` | Subrequest count by script name. (CFO-0047.01) |
| `cloudflare.workers.cpu_time` | `s` | CPU time by script name and statistic (p50, p75, p99, p999). (CFO-0047.01) |
| `cloudflare.workers.wall_time` | `s` | Wall time by script name and statistic (p50, p75, p99, p999). (CFO-0047.01) |
| `cloudflare.workers.request_duration` | `s` | Request duration by script name and statistic (p50, p75, p99, p999). (CFO-0047.01) |
| `cloudflare.certificate.pack` | `{pack}` | Present certificate packs (value 1), including packs with unknown expiry; latest complete snapshot only, expiring after three collector intervals. |
| `cloudflare.certificate.expiry` | `s` | Seconds until the earliest certificate expiry in a pack at collection time; negative once expired. Latest complete snapshot only, expiring after three collector intervals; packs with unknown expiry are omitted. (CFO-0050.01) |
| `cloudflare.tunnel.status` | `{tunnel}` | Current tunnel status gauge with value 1, by id, name and status. (CFO-0048.01) |
| `cloudflare.tunnel.connections` | `{connection}` | Active tunnel connections by id, name and colo. (CFO-0048.01) |
| `cloudflare.tunnel.connectors` | `{connector}` | Tunnel connectors by id, name and connector version. (CFO-0048.01) |
| `cf2otel.metric.cardinality_overflows` | `{datapoint}` | SDK overflow datapoints observed per export, by `cf2otel.instrument`. Cumulative overflow series are counted each time they are exported; the counter appears on the next collection. A warning names each overflowing instrument at most once per hour. (CFO-0045) |
| `cf2otel.identity.outcomes` | `{request}` | Monotonic counter of identity inference outcomes, grouped by `cf2otel.identity.outcome` (`matched`, `unmatched`, `ambiguous`). Records only new outcomes each poll, not repeated cumulative snapshots; resets on process restart. Replaces the three `cf2otel.identity.matched` / `unmatched` / `ambiguous` gauges. (CFO-0058) |
| `cloudflare.access.logins` | `1` | Human Access login count from `cf1AccessLoginsRawGroups`; `cloudflare.access.allowed` is normalized from the source decision to `true`/`false`, matching every Access signal. |
| `cloudflare.access.identity_logins` | `1` | Exact REST identity-login count by app, allowed, connection and action; excludes nonidentity service-token rows. |
| `cloudflare.access.requests` | `{request}` | Access request count from `accessLoginRequestsAdaptiveGroups`; keep `nonidentity` traffic separate. |
| `cloudflare.access.apps` | `1` | Access application inventory gauge. |
| `cloudflare.access.users` | `1` | Access user inventory gauge. |
| `cloudflare.access.seats` | `1` | Current user seat gauge by independent `access` or `gateway` type; overlapping types are not an additive billing-user total. |
| `cloudflare.http.requests` | `{request}` | Request count from sample-corrected `httpRequestsAdaptiveGroups`. |
| `cloudflare.http.origin.duration` | `s` | Average origin response duration per Groups window, in seconds. |
| `cloudflare.audit.events` | `1` | Exact audit event count by resource product, action type and action result. |
| `cloudflare.firewall.events` | `{event}` | Security event count from a Groups dataset by zone and available action/source. Optional rule ID/description, host and client country replace the default dimension set when `firewall.rule_dimensions` is enabled; the total per-window collector cap includes an all-other remainder. ByTimeGroups omits action/source, but may include advertised bot fields. `cloudflare.firewall.bot_score_bucket` and `cloudflare.firewall.bot_score_source` are selected by default only when advertised by the selected zone's actual dataset and when the field budget permits after existing fields. Buckets are exporter-defined width-10 numeric intervals over the uint8 domain: `0-9`, `10-19`, ... `240-249`, `250-255`. These are NOT vendor bot/human classifications or sentinel meanings. Only nonnegative integral scores in `0..255` are accepted; invalid, missing or null scores omit the bucket, while real zero belongs to `0-9`. Source names pass through as valid UTF-8 with no controls and at most 128 characters; a collector-lifetime sticky budget retains 32 normal names plus `other`, without enum guesses or identifier fallback. The existing default 500-series whole-attribute-set window cap includes bot dimensions and a count-conserving all-other remainder. Unadvertised or budget-skipped bot fields never prevent base counts or complete-window checkpoint advancement. |
| `cloudflare.dns.queries` | `1` | DNS query count from `dnsAnalyticsAdaptiveGroups` by zone and available query type, response code, cached/stale flags and protocol. Colo is omitted from metrics and remains on `cloudflare.dns.query` events. A collector-lifetime global budget admits at most 9998 normal attribute sets at default config and reserves one series containing only `cloudflare.dns.zone="<aggregated>"` for all other counts. Admissions are sticky across windows because the SDK retains cumulative series. The fallback preserves totals but loses every breakdown, including zone. Folding logs a warning naming this instrument at most once per hour; it is collector coalescing, not SDK overflow, and does not carry `otel.metric.overflow`. (CFO-0045) |
| `cloudflare.gateway.dns.queries` | `1` | Gateway DNS query sum from account-level `cf1GatewayDnsRawGroups`, by bounded query type, resolver decision and country. |
| `cloudflare.rum.page_views` | `1` | Page views from `rumPageloadEventsAdaptiveGroups` by country and device. |
| `cloudflare.rum.sessions` | `1` | Visit sum from `rumPageloadEventsAdaptiveGroups` by country and device. |
| `cloudflare.rum.lcp.p75` | `s` | Rolling p75 largest contentful paint gauge, converted from GraphQL microseconds to seconds. |
| `cloudflare.rum.inp.p75` | `s` | Rolling p75 interaction to next paint gauge, converted from GraphQL microseconds to seconds. |
| `cloudflare.rum.fid.p75` | `s` | Rolling p75 first input delay gauge, converted from GraphQL microseconds to seconds. |
| `cloudflare.rum.fcp.p75` | `s` | Rolling p75 first contentful paint gauge, converted from GraphQL microseconds to seconds. |
| `cloudflare.rum.ttfb.p75` | `s` | Rolling p75 time to first byte gauge, converted from GraphQL microseconds to seconds. |
| `cloudflare.rum.cls.p75` | `1` | Rolling p75 cumulative layout shift score gauge. |
| `cloudflare.workers.requests` | `{request}` | Worker request count from `workersOverviewRequestsAdaptiveGroups`, optionally by bounded script name. |
| `cloudflare.turnstile.events` | `1` | Turnstile event count from `turnstileAdaptiveGroups`, account aggregate. |
| `cloudflare.logpush.uploads` | `1` | Logpush upload count from `logpushHealthAdaptiveGroups`, account aggregate. |
| `cloudflare.logpush.records` | `1` | Logpush record count from `logpushHealthAdaptiveGroups`, account aggregate. |
| `cloudflare.d1.read_queries` | `1` | D1 read query sum from `d1AnalyticsAdaptiveGroups`, by bounded database name. |
| `cloudflare.d1.write_queries` | `1` | D1 write query sum from `d1AnalyticsAdaptiveGroups`, by bounded database name. |
| `cloudflare.d1.queries` | `1` | D1 query count from `d1QueriesAdaptiveGroups` by bounded database name; query text is not selected. |
| `cloudflare.d1.storage.max_database_bytes` | `By` | Maximum D1 database size across databases in the latest complete bucket, `By`. |
| `cloudflare.kv.requests` | `{request}` | KV operation request sum from `kvOperationsAdaptiveGroups`, by bounded namespace name. |
| `cloudflare.kv.storage.max_namespace_bytes` | `By` | Maximum KV namespace bytes in the latest complete bucket, `By`. |
| `cloudflare.kv.storage.max_namespace_keys` | `{key}` | Maximum KV namespace key count in the latest complete bucket. |
| `cloudflare.r2.bandwidth.download.bytes` | `By` | R2 download byte sum, optionally by bounded bucket name, `By`. |
| `cloudflare.r2.bandwidth.upload.bytes` | `By` | R2 upload byte sum, optionally by bounded bucket name, `By`. |
| `cloudflare.r2.catalog.data.operations` | `1` | R2 catalog data operation count, optionally by bounded namespace name. |
| `cloudflare.r2.catalog.maintenance.jobs` | `1` | R2 catalog maintenance job count, optionally by bounded namespace name. |
| `cloudflare.r2.requests` | `{request}` | R2 operation request sum by advertised action type and optional bucket name, bounded by complete attribute set. |
| `cloudflare.r2.storage.payload.bytes` | `By` | R2 payload-size gauge in the latest complete bucket per bucket, `By`. |
| `cloudflare.r2.storage.objects` | `{object}` | R2 object-count gauge in the latest complete bucket per bucket. |
| `cloudflare.r2sql.queries` | `1` | R2 SQL query count, optionally by bounded bucket name; table names are omitted. |
| `cloudflare.durableobjects.requests` | `{request}` | Durable Objects invocation request sum by bounded namespace name. |
| `cloudflare.durableobjects.subrequests` | `{request}` | Durable Objects periodic subrequest sum by bounded namespace name. |
| `cloudflare.durableobjects.sql_storage.max_namespace_bytes` | `By` | Maximum Durable Objects SQL storage across namespaces in the latest complete bucket, `By`. |
| `cloudflare.durableobjects.subrequests.request_body.bytes` | `By` | Durable Objects uncached request-body byte sum by bounded namespace name, `By`. |
| `cloudflare.queues.backlog.max_queue_avg_messages` | `{message}` | Maximum per-queue average backlog messages in the latest complete bucket. |
| `cloudflare.queues.backlog.max_queue_avg_bytes` | `By` | Maximum per-queue average backlog bytes in the latest complete bucket, `By`. |
| `cloudflare.queues.consumer.max_queue_avg_concurrency` | `{consumer}` | Maximum per-queue average consumer concurrency in the latest complete bucket. |
| `cloudflare.queues.delayed_backlog.max_queue_avg_messages` | `{message}` | Maximum per-queue average delayed backlog messages in the latest complete bucket. |
| `cloudflare.queues.message.operations` | `1` | Queue message operation count by bounded resolved queue name. |
| `cloudflare.queues.message.billable_operations` | `1` | Queue billable operation sum by bounded resolved queue name. |
| `cloudflare.email.routing.events` | `1` | Email Routing Groups count summed across account-owned zones, account aggregate. |
| `cloudflare.email.sending.events` | `1` | Email Sending Groups count summed across account-owned zones, account aggregate. |
| `cloudflare.ai_gateway.requests` | `{request}` | AI Gateway request count. |
| `cloudflare.ai_gateway.errors` | `1` | AI Gateway error count. |
| `cloudflare.ai_gateway.body_non_json` | `{request}` | Non-JSON bodies omitted from content export, counted per `cloudflare.ai_gateway.body.side` (`request` or `response`); metadata export continues. |
| `cloudflare.ai_gateway.cache_hits` | `1` | AI Gateway cache hits. |
| `cloudflare.ai_gateway.cost` | `1` | AI Gateway request cost. |
| `cloudflare.ai_gateway.dlp.requests` | `{request}` | AI Gateway request count with a DLP outcome (flagged, blocked or other), by gateway, action and direction; one count per distinct request/response direction, or `other` when the action carries no findings. |
| `cloudflare.ai_gateway.log_coverage.gap` | `{request}` | Gauge: `aiGatewayRequestsAdaptiveGroups` request count minus REST log count for one closed five-minute window, by `cloudflare.ai_gateway.gateway.name`. A completeness check on the REST log source, not a request rate. Off by default. |
| `gen_ai.client.operation.duration` | `s` | GenAI operation duration. |
| `gen_ai.client.inference.usage.input_tokens` | `{token}` | Input token usage. |
| `gen_ai.client.inference.usage.output_tokens` | `{token}` | Output token usage. |
| `gen_ai.client.inference.usage.cache_read.input_tokens` | `{token}` | Cached input tokens. |
| `gen_ai.client.inference.usage.reasoning.output_tokens` | `{token}` | Reasoning output tokens. |
| `gen_ai.client.inference.operation.input_tokens` | `{token}` | Input tokens by operation. |
| `gen_ai.client.inference.operation.output_tokens` | `{token}` | Output tokens by operation. |
| `cf2otel.scrape.success` | `1` | Collector scrape success state. |
| `cf2otel.zones.discovered` | `{zone}` | Zones returned by discovery for a complete collector poll. |
| `cf2otel.zones.filtered` | `{zone}` | Zones removed by include, ownership or exclusion selection, by reason. |
| `cf2otel.zones.processed` | `{zone}` | Distinct eligible zones queried in a complete collector poll. |
| `cf2otel.zones.skipped` | `{zone}` | Eligible zones not queried in a complete collector poll, by reason. |
| `cf2otel.scrape.duration` | `s` | Collector scrape duration. Explicit histogram boundaries in seconds: 0.25, 0.5, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 15, 20, 30, 45, 60, 90, 120. |
| `cf2otel.scrape.errors` | `1` | Collector scrape errors. |
| `cf2otel.scrape.last_success_timestamp` | `s` | Time of last successful collector scrape. |
| `cf2otel.export.success` | `1` | Successful OTLP exports. |
| `cf2otel.export.errors` | `1` | Failed OTLP exports. |
| `cf2otel.build.info` | `1` | Build identity. |
| `cf2otel.checkpoint.age` | `s` | Age of the oldest collector checkpoint. |
| `cf2otel.api.requests` | `{request}` | Cloudflare API requests, classified by actual HTTP status. |
| `cf2otel.api.envelope_errors` | `{error}` | Logical errors in unsuccessful Cloudflare API envelopes returned with successful HTTP status. Certificate pack permission code 9109 increments once with `cf2otel.status_class=4xx`; the HTTP request remains classified as 2xx. HTTP 403 is counted only by the shared HTTP request observer, not by this counter. |
| `cf2otel.api.duration` | `s` | Cloudflare API request duration. Explicit histogram boundaries in seconds: 0.25, 0.5, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 15, 20, 30, 45, 60, 90, 120. |
| `cf2otel.api.retries` | `1` | Cloudflare API retries. |
| `cf2otel.window.gap` | `s` | Skipped retention-gap seconds by collector. |
| `cf2otel.window.commit_failures` | `1` | Failed window commits by retry or dropped outcome. |
| `cf2otel.window.catchup_windows` | `1` | Additional bounded collector windows committed in one scheduler tick. |

Platform gauges select the latest complete five-minute source bucket and emit at export time. D1, KV, Durable Objects and Queue base gauges use the globally latest bucket across resources, then MAX within each resolved name or remainder; older resource buckets are not carried forward. R2 keeps its per-resource latest-bucket behavior. Base counters SUM within each resolved name or remainder. Queue averages still require the advertised queue ID for correct per-queue statistics; missing required fields fail before query, emission or checkpoint advancement. The emitter attaches the listed UCUM units to OTLP instruments. RUM timing values are converted from source microseconds to seconds before recording. The GenAI operation histogram and poller API/scrape histograms use explicit second-based buckets, including sub-second boundaries. Queue IDs are used only inside source aggregation and are never metric attributes.

Prometheus compatibility naming adds `_seconds` for `s` when the base name lacks it and `_ratio` for dimensionless gauges. Names already ending in `_bytes` retain that suffix, and annotated `{token}` and `{request}` units add no suffix. The generated Grafana dashboard (`dashboards/cf2otel.json`, built by `grafana/build_dashboard.py`) queries only the suffixed names.

AI Gateway metrics combine Cloudflare request outcome measurements with GenAI duration and usage conventions.

`cloudflare.ai_gateway.log_coverage.gap` comes from the separate `aigateway.coverage` collector, which is off by default. REST gateway logs stay the primary AI Gateway source; requests sent with log collection off, or dropped by gateway log limits or retention, never appear there, while `aiGatewayRequestsAdaptiveGroups` still counts them. Each commit compares one closed, aligned five-minute window ending at least 10 minutes before now, held back past the measured 139-365 s Groups ingestion lag, and reports one value per configured gateway: positive means Groups counted requests the REST log lacks, zero means the sources agree, and negative means Groups is behind REST. The Groups selection is `count` and `dimensions.gateway`, checked against the dataset's `availableFields`; a window older than the dataset's `notOlderThan` is skipped as a retention gap. The REST count pages the gateway log list and counts distinct log IDs. A failed commit retries the same window, and the gauge value for a window never accumulates. The collector polls every five minutes and commits five-minute windows whatever `interval` and `max_window` say, so each steady-state poll commits and exports one window; a poll that lands before the next window closes does nothing. During catch-up after an outage or at first start, several windows can commit before one metric export, so the exported gauge carries the newest committed window. An explicit collection range refuses any window that ends inside the 10-minute holdback. It also refuses a range spanning more than one closed five-minute window: the collector always registers with a five-minute `max_window`, so only an explicit `-since`/`-before` range (not the steady-state poll) can ask for more, and exporting just the first window while reporting success would silently hide the rest of the requested range. Request one five-minute window per call instead. Enable it in YAML with `enabled`, `interval`, `initial_lookback` and `max_window` under `collectors.aigateway.coverage`, or use environment overrides such as `CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__ENABLED=true`; configuration validation still requires a positive `interval` and `max_window`.

The retired AI Gateway dashboard's **Data boundaries** panel was static provenance guidance; **Gateway metadata exceptions** was a row grouping failed-request and DLP tables, not a separate API field. The AI Gateway Logs API inventory sampled on 2026-09-26 found no data-boundary or exception fields in 50 log-detail rows. Of that sample, 24 rows had a non-null `dlp_action` (`FLAG`), each with one `dlp_profiles` finding checking either the request (23) or the response (1); `dlp_action` maps to `flagged` (`FLAG`), `blocked` (`BLOCK`) or `other` (any other non-empty value), and a request with an action but no findings still counts once on `cloudflare.ai_gateway.dlp.requests` with direction `other`.

## Attributes

| Group | Keys |
| --- | --- |
| Resource and common | `service.name`, `service.version`, `service.instance.id`, `event_name`, `cf2otel.collector.name`, `cf2otel.status_class`, `cf2otel.api.method` |
| Access app and decision | `cloudflare.access.app`, `cloudflare.access.app.id`, `cloudflare.access.app.type`, `cloudflare.access.connection`, `cloudflare.access.host`, `cloudflare.access.path`, `cloudflare.access.action`, `cloudflare.access.allowed`, `cloudflare.access.country`, `cloudflare.access.login_type`, `cloudflare.access.identity_provider`, `cloudflare.access.service_token` |
| Access identity | `cloudflare.access.user.email`, `cloudflare.access.user.id`, `cloudflare.access.user.ip_address`, `cloudflare.access.identity.inferred`, `cloudflare.access.identity.login_ray_id`, `cloudflare.access.ray_id` |
| Access seats | `cloudflare.access.seat.type` (string enum `access`, `gateway`); no user, email, IP, device or account identity attributes. |
| Access SCIM | `cloudflare.access.scim.resource_type`, `cloudflare.access.scim.method`, `cloudflare.access.scim.status`, `cloudflare.access.scim.idp_id`, `cloudflare.access.scim.resource_id`, `cloudflare.access.scim.user_email` |
| HTTP | `cloudflare.http.host`, `cloudflare.http.method`, `cloudflare.http.path`, `cloudflare.http.query`, `cloudflare.http.status_code`, `cloudflare.http.origin_status_code`, `cloudflare.http.client_ip`, `cloudflare.http.user_agent`, `cloudflare.http.ray_id`, `cloudflare.http.zone`, `cloudflare.http.cache_status`, `cloudflare.http.security_action`, `cloudflare.http.colo` |
| AI Gateway content | `cloudflare.ai_gateway.content.side`, `cloudflare.ai_gateway.content.length` |
| AI Gateway non-JSON bodies | `cloudflare.ai_gateway.request.body_non_json`, `cloudflare.ai_gateway.response.body_non_json`: string `"true"` on the request log and span when that fetched body is not JSON. No content is exported from that side. `cloudflare.ai_gateway.body.side` is the counter dimension (`request` or `response`). |
| AI Gateway DLP | `cloudflare.ai_gateway.dlp.action`, `cloudflare.ai_gateway.dlp.direction` are also bounded metric dimensions on `cloudflare.ai_gateway.dlp.requests`; `cloudflare.ai_gateway.dlp.policy.id` and `cloudflare.ai_gateway.dlp.profile.id` are log and span attributes, never metric dimensions. |
| Audit | `cloudflare.audit.*` attributes are listed individually below; actor email and IP are log only. |
| Firewall | `cloudflare.firewall.*` attributes are listed individually below; IP, path, query, user agent and ray are log only. |
| DNS | `cloudflare.dns.*` attributes are listed individually below; query name, IPs and `cloudflare.dns.colo` are log only. |
| Gateway DNS | `cloudflare.gateway.dns.query.type`, `cloudflare.gateway.dns.decision`, `cloudflare.gateway.dns.country`; only bounded metric dimensions. |
| Platform resource names | `cloudflare.workers.script_name`, `cloudflare.r2.bucket_name`, `cloudflare.r2.catalog.namespace_name`, `cloudflare.r2sql.bucket_name`; bounded names only. |
| Platform resolved names | `cloudflare.d1.database_name`, `cloudflare.kv.namespace_name`, `cloudflare.queues.queue_name`, `cloudflare.durableobjects.namespace_name`; string names, never source IDs. |
| R2 operations | `cloudflare.r2.action_type`; string action type, selected only when advertised. |
| RUM | `cloudflare.rum.country`, `cloudflare.rum.device_type`, `cloudflare.rum.site_tag`; gauges use device and optional site tag. Web-vitals quantiles that are absent, null or negative emit no point, including when a previous window had a value; genuine zero measurements remain zero. |
| Window delivery | `cf2otel.window.*` describes retention gaps and commit outcomes. |
| Poller | `cf2otel.collector`, `cf2otel.version`, `cf2otel.commit`, `cf2otel.export.signal`, `cf2otel.build.version`, `cf2otel.build.commit` |

D1/KV/Queue/Durable Objects base metrics resolve IDs through read-only account list endpoints. Successful and failed name lookups are cached for one hour, shared within each domain. Requests start at 50 rows per page and follow server-capped page sizes, up to 100 pages and 5000 rows. Only a complete, consistent list is published; a later-page or refresh failure replaces any expired mapping with a failed state, yielding `other` for one hour rather than retaining stale names indefinitely. Blank, invalid UTF-8, over-128-character, reserved `other`, unknown and ambiguous names yield `other`; IDs never become a fallback label. Optional unavailable grouping fields yield a safe remainder; required Queue statistics fields remain mandatory.

Each affected base metric admits 49 normal complete attribute sets for the collector lifetime plus one reserved `other` set (50 total). Admission stays sticky across windows, bounding cumulative SDK series growth until restart. R2 operations count the bucket/action pair, so different actions on one bucket remain distinct until capped; the canonical remainder sets every present resource/action dimension to `other` and conserves counter sums (gauge remainder is MAX). The existing total platform per-window cap defaults to 500 and applies deterministic full-attribute ordering when truncation is necessary. Existing metric names, units and depth quantile selections are unchanged; percentiles are never averaged. Operator-defined resource names may be sensitive: review them before export. Dashboard panels and live/canary entitlement proof are separate root-owned work.

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
| Metric | `cf2otel.api.envelope_errors` |
| Metric | `cf2otel.api.retries` |
| Metric | `cf2otel.build.info` |
| Metric | `cf2otel.checkpoint.age` |
| Metric | `cf2otel.export.errors` |
| Metric | `cf2otel.export.success` |
| Metric | `cf2otel.identity.outcomes` |
| Attribute | `cf2otel.identity.outcome` |
| Attribute | `cf2otel.error.class` |
| Metric | `cf2otel.scrape.duration` |
| Metric | `cf2otel.scrape.errors` |
| Metric | `cf2otel.scrape.last_success_timestamp` |
| Metric | `cf2otel.scrape.success` |
| Metric | `cf2otel.zones.discovered` |
| Metric | `cf2otel.zones.filtered` |
| Metric | `cf2otel.zones.processed` |
| Metric | `cf2otel.zones.skipped` |
| Attribute | `cf2otel.zone.reason` |
| Metric | `cf2otel.window.commit_failures` |
| Metric | `cf2otel.window.catchup_windows` |
| Metric | `cf2otel.window.gap` |
| Metric | `cloudflare.access.apps` |
| Metric | `cloudflare.access.identity_logins` |
| Metric | `cloudflare.access.logins` |
| Metric | `cloudflare.access.requests` |
| Metric | `cloudflare.access.users` |
| Metric | `cloudflare.access.seats` |
| Metric | `cloudflare.ai_gateway.cache_hits` |
| Metric | `cloudflare.ai_gateway.cost` |
| Metric | `cloudflare.ai_gateway.dlp.requests` |
| Metric | `cloudflare.ai_gateway.errors` |
| Metric | `cloudflare.ai_gateway.body_non_json` |
| Metric | `cloudflare.ai_gateway.log_coverage.gap` |
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
| Attribute | `cloudflare.d1.database_name` |
| Attribute | `cloudflare.kv.namespace_name` |
| Attribute | `cloudflare.queues.queue_name` |
| Attribute | `cloudflare.durableobjects.namespace_name` |
| Attribute | `cloudflare.r2.action_type` |
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
| Attribute | `cloudflare.access.seat.type` |
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
| Attribute | `cloudflare.ai_gateway.request.body_non_json` |
| Attribute | `cloudflare.ai_gateway.body.side` |
| Attribute | `cloudflare.ai_gateway.request.content_type` |
| Attribute | `cloudflare.ai_gateway.request.head` |
| Attribute | `cloudflare.ai_gateway.request.head_complete` |
| Attribute | `cloudflare.ai_gateway.request.size` |
| Attribute | `cloudflare.ai_gateway.request.type` |
| Attribute | `cloudflare.ai_gateway.response.body` |
| Attribute | `cloudflare.ai_gateway.response.body_truncated` |
| Attribute | `cloudflare.ai_gateway.response.body_unavailable` |
| Attribute | `cloudflare.ai_gateway.response.body_non_json` |
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
| Attribute | `cloudflare.firewall.bot_score_bucket` |
| Attribute | `cloudflare.firewall.bot_score_source` |
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
| Attribute | `cloudflare.firewall.rule_description` |
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
| Attribute | `gen_ai.request.model`: AI Gateway model identifier with the `provider/` prefix removed; unprefixed identifiers and `@` model namespaces are preserved. Empty values, empty path segments, null-like placeholders and non-identifier strings (such as prose, JSON or URLs) map to `unknown`. Identifiers may contain ASCII letters, digits, `-`, `_`, `.`, `:`, `@` and namespace `/` separators; validation is syntactic, not a model-catalog lookup. The same normalized value is used in logs, spans (including their names), and model-level metrics. |
| Attribute | `gen_ai.usage.cache_read.input_tokens` |
| Attribute | `gen_ai.usage.cost` |
| Attribute | `gen_ai.usage.input_tokens` |
| Attribute | `gen_ai.usage.output_tokens` |
| Attribute | `gen_ai.usage.reasoning.output_tokens` |
| Attribute | `outcome` |
| Attribute | `service.instance.id` |
| Attribute | `service.name` |
| Attribute | `service.version` |

## Loop 14 attribute seams

| Attribute | Meaning |
| --- | --- |
| `cf2otel.identity.outcome` | Identity inference outcome: matched, unmatched or ambiguous. (CFO-0058) |
| `cloudflare.statistic` | Statistic: avg, p50, p75, p95, p99 or p999. (CFO-0046.04 / CFO-0047.01) |
| `cloudflare.workers.status` | Invocation status as returned. (CFO-0047.01) |
| `cf2otel.instrument` | Overflowing instrument name. (CFO-0045) |
| `cloudflare.http.client.country` | Uppercase ISO 3166-1 alpha-2 country. (CFO-0046.02) |
| `cloudflare.http.protocol` | HTTP protocol as returned, e.g. HTTP/2. (CFO-0046.02) |
| `cloudflare.http.tls.protocol` | TLS protocol as returned, e.g. TLSv1.3 or none. (CFO-0046.02) |
| `cloudflare.http.content_type` | Edge response content type name. (CFO-0046.02) |
| `cloudflare.certificate.zone` | Certificate pack zone name. (CFO-0050.01) |
| `cloudflare.certificate.pack_id` | Certificate pack pack_id. (CFO-0050.01) |
| `cloudflare.certificate.type` | Certificate pack type. (CFO-0050.01) |
| `cloudflare.certificate.authority` | Certificate pack authority. (CFO-0050.01) |
| `cloudflare.certificate.status` | Certificate pack status. (CFO-0050.01) |
| `cloudflare.tunnel.id` | Tunnel id. (CFO-0048.01) |
| `cloudflare.tunnel.name` | Tunnel name. (CFO-0048.01) |
| `cloudflare.tunnel.status` | Tunnel status. (CFO-0048.01) |
| `cloudflare.tunnel.previous_status` | Tunnel previous_status. (CFO-0048.01) |
| `cloudflare.tunnel.colo` | Tunnel colo. (CFO-0048.01) |
| `cloudflare.tunnel.connector.version` | Tunnel connector.version. (CFO-0048.01) |


## Collector scrape error classes

Registration seeds `cf2otel.scrape.errors` with one zero-valued point per
registered, enabled collector, using `cf2otel.error.class="other"`. This makes
never-failing collectors visible without recording a failure. Window collectors
also receive a zero-valued `cf2otel.window.commit_failures` point with only
`cf2otel.collector`; actual commit failures continue to carry their existing
signal and retry/dropped outcome dimensions. Disabled collectors get no seed,
and disabling periodic self-observability snapshots does not disable these
registration points. Dry-run does not export or persist them.

`cf2otel.scrape.errors` remains a monotonic counter with unit `1`: each failed
collector attempt adds one, grouped by `cf2otel.collector` and the bounded
`cf2otel.error.class` attribute. Successful attempts do not add an error. No raw
error message, upstream field name, response body or HTTP code becomes an attribute.

| Attribute | Values |
| --- | --- |
| `cf2otel.error.class` | `rate_limited`, `auth`, `unentitled`, `timeout`, `schema`, `other` |

Classification follows typed errors through wrappers. Explicit typed entitlement
errors take precedence over HTTP status: unavailable GraphQL fields or disabled
datasets are `unentitled`. HTTP 429 is `rate_limited`; HTTP 401/403 is `auth`
unless a typed entitlement error is present. Context deadline exhaustion and
network timeout errors are `timeout`. GraphQL field-limit errors and JSON syntax
or type errors are `schema`. Retention gaps, saturated windows, cancellation,
other HTTP failures and unknown errors are `other`; message text is never used
to infer a class. Classification observes failures without changing retries,
permission handling, checkpoint decisions or collector scheduling.

These labels apply only to collector scrape errors, not export errors or scrape
duration/success metrics. Existing aggregate queries can continue to sum scrape
errors across classes. This is the code/documentation portion of CFO-0051.03
(classify collector errors); the dashboard panel is delivered separately.

## Opt-in HTTP colo, ASN and safe error routes

| Name | Kind / unit | Attributes |
| --- | --- | --- |
| `cloudflare.http.requests.by_colo` | Counter / `{request}` | `cloudflare.http.zone`, `cloudflare.http.colo`, remainder. |
| `cloudflare.http.requests.by_asn` | Counter / `{request}` | Zone, string ASN, string ASN description, remainder. Both ASN fields must be advertised; live-verified on the eligible HTTP Groups zone, not universally available. |
| `cloudflare.http.errors.by_route` | Counter / `{request}` | Zone, configured safe route name, status class, remainder. Counts only 4xx/5xx groups; paths are internal lookup inputs, never attributes. |

| Attribute | String value contract |
| --- | --- |
| `cloudflare.http.client.asn` | Advertised source ASN string, without numeric coercion; `other` in the remainder. |
| `cloudflare.http.client.asn_description` | Advertised source organization-description string; `other` in the remainder. |
| `cloudflare.http.route.name` | Validated configured safe name; `other` for unmatched and overflow counts. Never a raw or normalized path. |
| `cloudflare.http.status_class` | `4xx`, `5xx`; `other` in the remainder. |
| `cloudflare.http.breakdown.remainder` | `false` for normal tuples; `true` for the single all-other tuple. A real source colo/ASN named `other` remains distinct via `false`. |

All three toggles are absent by default. Per-feature/zone lexical caps conserve counts in
one remainder, and every point counts against the existing complete-window HTTP cap.
An optional exact host allowlist filters only these features and adds no host label.
See [configuration](configuration.md#opt-in-high-cardinality-http-breakdowns) for limits,
normalizer behavior and failure semantics. Dashboard delivery is a separate signal batch.
