---
id: doc-0003
title: Cloudflare API surface - live-verified reference
type: specification
created_date: '2026-09-23 09:59'
updated_date: '2026-09-26 15:04'
---
Live-verified against a real non-Enterprise account (one Pro zone, twenty-odd Free zones, Zero Trust
Free, one AI Gateway) on **2026-09-23** with a read-only token. Where Cloudflare's documentation and
the live API disagree, **the live API wins**; this page records what the API actually did.

Re-verify a fact before building on it if it is more than a wave old. The API drift canary
(`cfo` task "API drift canary") exists to catch these going stale.

## 1. The discovery mechanism: GraphQL `settings`

Every GraphQL dataset has a `settings` node per account (`viewer.accounts[].settings.<dataset>`) and
per zone (`viewer.zones[].settings.<dataset>`) exposing:

| Field | Meaning |
|---|---|
| `enabled` | whether this account/zone may query the dataset at all |
| `notOlderThan` | retention in seconds (how far back a query may reach) |
| `maxDuration` | widest single query window in seconds |
| `maxPageSize` | largest `limit` |
| `maxNumberOfFields` | most leaf fields one query may select (30 account-level, 70 zone-level observed) |
| `availableFields` | advertised field paths for the account/zone; verify disputed fields against the live query schema |

**cf2otel builds GraphQL selections from `availableFields` intersected with the fields it
wants**, per zone. On 2026-09-23 the firewall datasets advertised fields that their live query
schema rejected, so the intersection is a necessary entitlement check but not sufficient proof of
query validity. Entitlement is per field and per plan: the same query failed on the Pro zone
(`does not have access to the field 'wafattackscoreclass'`) and on a Free zone (`... field 'ja4'`),
and one unentitled field fails the **whole** query, not just that column.

Counts on the verified account: 232 account datasets (180 enabled), 58 zone datasets (~45 enabled on
both Pro and Free). Enumerate them live with the introspection of `AccountSettings` / `ZoneSettings`
rather than trusting any list here.

## 2. Datasets in scope, with verified limits

Retention / max window, as `notOlderThan` / `maxDuration`.

### Access (account)

| Dataset | Retention / window | Notes |
|---|---|---|
| REST `GET /accounts/{a}/access/logs/access_requests` | see trap 3 | identity logins: `user_email`, `user_id`, `ip_address`, `app_uid`, `app_name`, `app_domain`, `app_type`, `action`, `connection`, `allowed`, `created_at`, `ray_id`, `country` |
| GraphQL `cf1AccessLoginsRawGroups` | 31d / **3h** | dims `allowed appName appType country datetime datetimeFiveMinutes datetimeHour idp loginType`; metric is `sum { logins }` (there is **no** top-level `count`) |
| GraphQL `cf1AccessLogins1hGroups` / `1dGroups` | 62d / 31d, 365d / 90d | pre-aggregated rollups |
| GraphQL `accessLoginRequestsAdaptiveGroups` | 90d / 7d | dims include `appId approvingPolicyId cfRayId country deviceId hasExistingJWT hasGatewayEnabled hasWarpEnabled identityProvider ipAddress isSuccessfulLogin mtls* serviceToken* userUuid`. On the verified account **every row was `identityProvider=nonidentity` with an empty `userUuid`**: it carries service-token and bypass traffic, not user logins |
| REST `GET /accounts/{a}/access/logs/scim/updates` | not measured | `cf_resource_id http_method idp_id idp_resource_id logged_at resource_type resource_user_email status` |
| REST `GET /accounts/{a}/access/apps`, `/access/users` | inventory | app names/domains for enrichment; users carry last-seen data |

### HTTP requests (zone)

| Dataset | Retention / window | Notes |
|---|---|---|
| `httpRequestsAdaptive` | 31d / 30d, on **Free and Pro** | raw sampled per-request events: host, method, path, query, status, origin status and timings, client IP, ASN, country, UA, cache status, colo, security action/source and more, subject to `availableFields`. **No user identity field** (see trap 5) |
| `httpRequestsAdaptiveGroups` | 31d / 30d | aggregates for metrics |
| `httpRequests1hGroups` | 7d Pro, 3d Free | rollup |
| `httpRequests1mGroups` | Pro only, 1d | |
| `firewallEventsAdaptive` | 31d / 30d, Free and Pro | raw security events (wave 2); `clientAsnDescription` is advertised in settings but rejected by the live query schema |
| `firewallEventsAdaptiveGroups` | Pro only, 3d | `count` with `dimensions.action` and `dimensions.source` accepted in a live query |
| `firewallEventsAdaptiveByTimeGroups` | Free fallback | `count` accepted; `dimensions.action` and `dimensions.source` rejected despite settings advertisement |
| `dnsAnalyticsAdaptive` | 31d | raw DNS queries (wave 2) |

### AI Gateway (account)

| Surface | Notes |
|---|---|
| REST `GET /accounts/{a}/ai-gateway/gateways` | gateway list incl. `collect_logs`, log retention cap (`log_management`, e.g. 100000) and strategy (`DELETE_OLDEST`) |
| REST `GET .../gateways/{g}/logs` | page-based (`page`, `per_page`, `result_info.total_count`), `order_by=created_at`, `order_by_direction=asc\|desc`, `start_date`/`end_date` RFC3339. The live endpoint accepts `per_page=50` and rejects `per_page=100` with HTTP 400 code 7001 (2026-09-23). Row fields: `id` (ULID), `created_at`, `event_id`, `provider`, `model`, `model_type`, `path`, `duration`, `request_type`, `status_code`, `success`, `cached`, `tokens_in`, `tokens_out`, `usage_metadata{input_tokens,output_tokens,total_tokens,output_reasoning_tokens,input_cached_tokens}`, `timings{total,latency}`, `location{region,colo}`, `cost`, `custom_cost`, `metadata`, `step`, `feedback`, `score`, `prompts`, `guardrails`, `authentication`, `wholesale`, `byok`, `user_agent`, `dlp_action`, `dlp_profiles`. `request`/`response` are **empty strings in the list** |
| REST `GET .../logs/{id}` | adds `request_head`, `response_head`, `*_head_complete`, `request_size`, `response_size`, `request_content_type` |
| REST `GET .../logs/{id}/request`, `/response` | the full raw bodies (prompt / completion JSON), returned as raw JSON without the usual `result` envelope. One extra call per body. Either body endpoint can return HTTP 404 code 7002 for a listed log; preserve the log and mark that side unavailable |
| GraphQL `aiGatewayRequestsAdaptiveGroups` | 62d / 32d. Dims `cached cost date datetime* durationMs error gateway model provider rateLimited statusCode tokensIn tokensOut userAgent wholesale metadata* prompts* ...`, `sum { cost tokensIn tokensOut }`. See trap 7 |
| GraphQL `aiGatewayErrorsAdaptiveGroups`, `aiGatewayCacheAdaptiveGroups`, `aiGatewaySizeAdaptiveGroups` | 32d |

Token permission groups needed for the full read surface: `AI Gateway Read` (bodies); `AI Gateway
Metadata Read` alone lists logs but **403s on bodies**.

### Account audit (wave 2)

REST `GET /accounts/{a}/logs/audit?since=&before=&limit=` (v2) is cursor-paginated via
`result_info.cursor`; rows are `{id, account, action{description,result,time,type}, actor{...},
raw{...}, resource{...}}`. The legacy `GET /accounts/{a}/audit_logs` also still answers. GraphQL
`auditLogsGroups` has 540d retention.

### Other enabled account datasets worth a later wave

Gateway (`cf1GatewayDns*`, `cf1GatewayHttp*`, `cf1GatewayNetwork*`, `gatewayResolver*`, `gatewayL4/L7`),
`workersInvocationsAdaptive`, `rumPageloadEventsAdaptiveGroups` / `rumWebVitalsEventsAdaptive*`
(184d), `turnstileAdaptiveGroups`, `emailRoutingAdaptive` / `emailSendingAdaptive` (zone),
`dmarcReportsAdaptive` (zone), D1 / KV / R2 / Durable Objects / Queues / Workflows analytics,
`logpushHealthAdaptiveGroups`, `cloudflareTunnelsAnalyticsAdaptiveGroups`.

Not available without Enterprise on the verified account: Logpull (`/zones/{z}/logs/received`), and
**listing Logpush jobs** (`/accounts/{a}/logpush/jobs` answered `10000 Authentication error` even with
`Logs Read`).

## 3. Traps (each one defeats a plausible-but-wrong implementation)

1. **One unentitled field fails the whole GraphQL query.** Build selections from `availableFields`,
   per zone, every run (plans change).
2. **`maxNumberOfFields` caps a selection** (30 at account level, 70 at zone level observed). A wide
   selection returns `number of fields can't be more than 30`. Split into several queries or trim.
3. **The Access REST log has short, undocumented reach.** `limit=1000` with no window returned only
   ~30 hours of rows; a `since/until` window 22 days back returned **zero** rows. Retention was not
   pinned down. Poll often (minutes), persist the cursor, and never assume a restart can backfill
   more than about a day. `result_info.total_count` came back `0` alongside 25 rows: do not paginate
   on it.
4. **`app_domain` in Access REST rows is a URL prefix, not a host.** Rows carried
   `host/assets/<file>.js` and `host/api/...` paths, one row per sub-resource. Split host from path
   before using it as an attribute, and never make it a metric attribute.
5. **Per-request HTTP events have no Access identity.** `httpRequestsAdaptive` has no user field
   (`fraudUserId` is fraud-detection, not Access). Identity is *inferred* by joining client IP + host +
   time against Access login events; the inference must be labelled as such.
6. **Adaptive datasets are sampled.** Raw events are a sample, not a census. Record
   `sampleInterval` where the dataset exposes it (it is **not** a field on `httpRequestsAdaptive`),
   derive rate metrics from the `*Groups` datasets (whose `count` is already sample-corrected), never
   by counting raw rows.
7. **AI Gateway GraphQL Groups can arrive late.** In a read-only 2026-09-23 check, a 3-hour
   `aiGatewayRequestsAdaptiveGroups` window returned zero while REST listed 38 requests; the newest REST
   request was already 13.9 minutes old. Repeating the identical GraphQL window later returned 38.
   Separate 1-hour and 3-hour comparisons matched REST at 2/2 and 39/39. The upper ingestion-lag
   bound remains unknown, so wave 1 leaves `aigateway.metrics` unregistered and derives bounded metrics
   from the verified REST log. Do not interpret a fresh empty Groups result as zero activity.
8. **Service-token traffic dominates Access analytics.** One app produced ~210,000 `nonidentity`
   logins in six days against a few hundred for everything else. Keep `nonidentity` separable from
   identity logins in every signal.
9. **A newly minted Cloudflare token flaps for about a minute** (403 on one zone, 200 on another, then
   swapped). Retry before concluding a permission is missing.
10. **GraphQL field names are case-insensitive in error text** (`wafattackscoreclass`): match
    entitlement errors case-insensitively.

## 4. Native AI Gateway OpenTelemetry export (what cf2otel replaces)

Cloudflare's own AI Gateway OTLP export sends one span per request, name `cf.aig.request`,
`service.name=ai-gateway`, no parent, with exactly: `gen_ai.operation.name`, `gen_ai.request.model`,
`gen_ai.provider.name`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`,
`gen_ai.input.messages`, `gen_ai.output.messages` (raw request/response JSON) and `gen_ai.usage.cost`.
It carries no cache, status, colo, latency breakdown, reasoning or cached-token split, metadata, DLP,
guardrail, BYOK or retry (`step`) data, and cannot backfill. cf2otel's GenAI output must be a strict
superset of it.

## 7. Additional live observations on 2026-09-24

- Gateway DNS: `cf1GatewayDnsRawGroups` and `cf1GatewayDns1dGroups` returned nonzero query sums; Gateway HTTP and network Groups returned zero rows in both a recent two-hour window and an 89-day window. The DNS Groups count is `sum.queries`; advertised optional dimensions include `queryType`, `resolverDecision` and `country`. The live account returned resolver decisions `allowedOnNoRule` and `overrideRule`; retain bounded handling for these observed values even though Cloudflare's published resolver-decision table uses a different set of names.
- HTTP metrics: `httpRequestsAdaptiveGroups` was enabled with required count/host/status/cache fields on all 23 zones visible to the configured account. An optional `avg.originResponseDurationMs` value of `-1` occurred in live Groups rows. It is excluded from the origin-duration gauge while the row's request count is retained. Filter `/zones` results by the configured account before querying all-zone or explicitly selected metrics zones, since token-visible zone inventory can span accounts.
- Audit v2: 369 fresh source events were first observed 167-450 seconds after their event time (median 289 seconds, p95 402 seconds). A two-minute collector holdback was therefore shorter than observed visibility lag. A ten-minute forward holdback is deployed without backfill or checkpoint rewind; the first complete post-fix UTC hour had 248 source IDs and 248 matching Loki IDs, with the second hour pending at that observation; section 8 records its later proof.

## 8. Loop 4 live observations on 2026-09-24

- Gateway DNS: after v0.4.0 deployment, `cf1GatewayDnsRawGroups` returned two rows totaling 2773 queries in a 30-minute post-deploy window. Mimir had eight `cloudflare_gateway_dns_queries_total` series and a 2899.151 counter increase; the 126.151 difference was smaller than either adjacent five-minute source bucket. Gateway HTTP and network Groups returned zero rows in the post-deploy 24-hour source recheck, so those branches remain unbuilt.
- HTTP all-zone metrics: `http.metrics_scope: all` expanded distinct `cloudflare_http_zone` values from two to eleven on equal ten-minute pre/post windows. The post window had 29 active request series, below the 10000-series cap, with zero `httpreq.metrics` window-commit failures. A read-only `httpRequestsAdaptiveGroups` source query on the post-restart interval returned 2230 requests, while `sum(max_over_time(cloudflare_http_requests_total[20m]))` after delivery returned 2260. Counter samples are stamped at export time, so querying `increase` at the source window end before its checkpoint advances can substantially undercount newly appearing sparse series.
- AI Gateway update: a fresh gateway GET exposed 23 settable fields after excluding `id`, `created_at`, `modified_at`, `internal` and `wholesale`. Three settable fields were null: `rate_limiting_interval`, `rate_limiting_limit` and `logpush_public_key`. A no-op PUT retaining them succeeded with a 2xx response (exact code was not retained); re-GET changed only `modified_at`. A PUT changing only `otel` to `[]` returned HTTP 200 and re-GET showed only `otel` and `modified_at` changed. An authorized rollback PUT returned HTTP 200 and restored the original gateway configuration apart from `modified_at`. No API error body exists for these accepted requests.
- Workers observability destinations: two destination objects had identical stable configuration before cutover, after cutover and after rollback. Their `configuration.jobStatus.last_complete` values advanced independently during the readbacks. Comparing the whole destination objects as immutable configuration caused a false rollback; compare the destination configuration while excluding only this operational timestamp. The native AI Gateway export remains enabled after rollback.
- Audit v2 forward proof: the second complete post-holdback UTC hour had 232 source IDs and 232 matching Loki rows and IDs, with zero missing, extra or duplicate IDs. The prior hour was 248/248.

### Loop 6 AI Gateway cutover (2026-09-25)

The running v0.5.1 collector passed a complete 60-minute source-ID window: six AI Gateway log IDs, six Loki request rows and six direct Tempo spans, one of each per ID and no extras. A fresh Gateway GET had one native `otel` entry and two Workers observability destinations. The no-op PUT returned HTTP 200; re-GET changed only `modified_at`. The cutover PUT returned HTTP 200; re-GET changed only `otel` to `[]` and `modified_at`. Both Workers destinations kept their stable configuration.

The accepted PUT body retained these 23 settable fields: `authentication`, `byok_only`, `cache_invalidate_on_update`, `cache_ttl`, `collect_logs`, `dlp`, `is_default`, `log_classification`, `log_management`, `log_management_strategy`, `logpush`, `logpush_public_key`, `otel`, `rate_limiting_interval`, `rate_limiting_limit`, `rate_limiting_technique`, `retry_backoff`, `retry_delay`, `retry_max_attempts`, `spend_limits`, `store_id`, `workers_ai_billing_mode`, `zdr`. The three null fields (`logpush_public_key`, `rate_limiting_interval`, `rate_limiting_limit`) stayed present. Compare Workers destinations by dropping only the whole `configuration.jobStatus` subtree; its completion timestamp changes independently of configuration.

In the settled post-cutover window [13:59:45Z,14:29:45Z), three source IDs each had one Loki row and one direct cf2otel Tempo span, with no extras. Native AI Gateway Tempo spans were zero; the preceding control window had seven source IDs and six native spans. No rollback PUT was sent. The first read-only watch query used a Tempo limit above the endpoint's 1000-result cap and failed HTTP 400; the corrected bounded query returned six control spans and the watcher passed at 14:48:46Z.

## 9. Loop 5 platform Groups selections (2026-09-25)

The built platform collectors use account-scoped GraphQL Groups datasets. Every query selects
`dimensions.datetimeFiveMinutes` and only complete five-minute buckets. Counters sum additive fields;
storage gauges take the latest complete bucket. Metric points are stamped at export time, and
`1`/`By` are intended units rather than OTLP instrument unit metadata in the current emitter.
These are implemented selections, not proof of deployed values; source-to-Mimir comparisons remain pending.

| Dataset | Selected value fields | Aggregation and optional selection |
| --- | --- | --- |
| `workersOverviewRequestsAdaptiveGroups` | `count` | Sum; optional `dimensions.scriptName` becomes a bounded metric attribute. |
| `turnstileAdaptiveGroups` | `count` | Account sum; no resource attribute. |
| `logpushHealthAdaptiveGroups` | `sum.uploads`, `sum.records` | Account sums; no job identifier. |
| `d1AnalyticsAdaptiveGroups` | `sum.readQueries`, `sum.writeQueries` | Account sums. |
| `d1QueriesAdaptiveGroups` | `count` | Account sum; query text is never selected. |
| `d1StorageAdaptiveGroups` | `max.databaseSizeBytes` | Maximum across databases in latest bucket; no database identifier. |
| `kvOperationsAdaptiveGroups` | `sum.requests` | Account sum. |
| `kvStorageAdaptiveGroups` | `max.byteCount`, `max.keyCount` | Maxima across namespaces in latest bucket; no namespace identifier. |
| `r2BandwidthUsageAdaptiveGroups` | `sum.bytesDownload`, `sum.bytesUpload` | Sums; optional bounded `dimensions.bucketName`. |
| `r2CatalogDataOperationsAdaptiveGroups` | `count` | Sum; optional bounded `dimensions.namespaceName`; no table name. |
| `r2CatalogTableMaintenanceAdaptiveGroups` | `count` | Sum; optional bounded `dimensions.namespaceName`; no table name. |
| `r2OperationsAdaptiveGroups` | `sum.requests` | Sum; optional bounded `dimensions.bucketName`. |
| `r2StorageAdaptiveGroups` | `max.payloadSize`, `max.objectCount` | Latest complete bucket per bucket; optional bounded `dimensions.bucketName`. |
| `r2sqlOperationsAdaptiveGroups` | `count` | Sum; optional bounded `dimensions.bucket`; no table name. |
| `durableObjectsInvocationsAdaptiveGroups` | `sum.requests` | Account sum. |
| `durableObjectsPeriodicGroups` | `sum.subrequests` | Account sum. |
| `durableObjectsSqlStorageGroups` | `max.storedBytes` | Maximum across namespaces in latest bucket; no namespace attribute. |
| `durableObjectsSubrequestsAdaptiveGroups` | `sum.requestBodySizeUncached` | Account sum. |
| `queueBacklogAdaptiveGroups` | `avg.messages`, `avg.bytes` | Latest-bucket maximum queue averages; `dimensions.queueId` groups internally only. |
| `queueConsumerMetricsAdaptiveGroups` | `avg.concurrency` | Latest-bucket maximum queue average; `dimensions.queueId` groups internally only. |
| `queueDelayedBacklogAdaptiveGroups` | `avg.messages` | Latest-bucket maximum queue average; `dimensions.queueId` groups internally only. |
| `queueMessageOperationsAdaptiveGroups` | `count`, `sum.billableOperations` | Account sums; no queue identifier emitted. |

Read-only type introspection on 2026-09-25 found `queueId` and no `queueName` in all four Queue
Groups dimension types. Settings for all four were enabled and advertised `dimensions_queueId` and
`dimensions_datetimeFiveMinutes`. The three Queue gauges require `queueId` internally to establish
a maximum per queue; no Queue identifier reaches a metric attribute or log. The collectors fail a
window that saturates at one complete five-minute bucket rather than split that aggregate.

Zone-scoped email presence was rechecked over 30 days on 2026-09-25: routing and sending Groups
variants had rows; the DMARC dataset had none. The Groups-backed routing and sending collectors shipped in v0.6.0 and the Camden deployment is healthy; source-to-Mimir equality remains unproved. They select only `count` and `dimensions.datetimeFiveMinutes`, sum complete five-minute buckets across account-owned zones, and emit no zone metric attribute. A disabled zone is skipped. No DMARC collector is built.

| Zone dataset | Selected fields | E24-A source finding |
| --- | --- | --- |
| `emailRoutingAdaptiveGroups` | `count`, `dimensions.datetimeFiveMinutes` | Enabled Groups variant had rows on two zones in the 30-day census. |
| `emailSendingAdaptiveGroups` | `count`, `dimensions.datetimeFiveMinutes` | Enabled Groups variant had rows on two zones in the 30-day census. |

## 10. Loop 7 observations (2026-09-26)

- The first scheduled Cloudflare API drift canary after the read-only token permission edit and contract update concluded successfully against the deployed contract. Its probe reported `Cloudflare API contract matched`; the workflow uploaded no artifact, and its downloaded log had zero credential-byte matches. This verifies the canary run, not all future API behavior.
- AI Gateway DLP was enabled with one Flag policy, three selected profiles and both request/response inspection. Fifty recent gateway log-detail rows had `dlp_action: null` and `dlp_profiles: null`. Six short requests using only published fictional payment-card test values returned gateway log IDs; the five inspected response headers had no `cf-aig-dlp`, and each exact log detail still had both DLP fields null. The first response header was not inspected. No matched Logs API row or exact non-null field shape was observed, so policy ID and request/response direction mapping remain unverified. The retired dashboard's “Data boundaries” panel was static provenance text and “Gateway metadata exceptions” was a grouping of failure and DLP panels, not independent Logs API fields.
- The first complete post-deploy email Groups window [11:30Z, 11:40Z) had both datasets enabled on all 23 account-owned zones and zero rows/count for both. A same-selection prior-seven-day census found a routing row on day offset zero and no sending row. The extended [11:30Z, 14:30Z) window, read after both checkpoints and exporter delivery, had routing count 13 in six source rows and sending count zero across 23 enabled owned zones. The routing Mimir counter was 16: a wider [11:00Z, 14:30Z) source read counted 16, explaining the three-event startup backfill before 11:30Z. This is aggregate routing evidence, not an exact same-window delta. Sending had no source row over the prior seven days with the same selection, so its zero remains a defect candidate and source-to-Mimir acceptance is open.
