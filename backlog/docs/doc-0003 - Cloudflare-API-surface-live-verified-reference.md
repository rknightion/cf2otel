---
id: doc-0003
title: Cloudflare API surface - live-verified reference
type: specification
created_date: '2026-09-23 09:59'
updated_date: '2026-09-23 10:01'
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
| `availableFields` | the exact field paths this account/zone is entitled to |

**cf2otel must build every GraphQL selection from `availableFields` intersected with the fields it
wants**, per zone. Entitlement is per field and per plan: the same query failed on the Pro zone
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
| `firewallEventsAdaptive` | 31d / 30d, Free and Pro | raw security events (wave 2) |
| `firewallEventsAdaptiveGroups` | Pro only, 3d | |
| `dnsAnalyticsAdaptive` | 31d | raw DNS queries (wave 2) |

### AI Gateway (account)

| Surface | Notes |
|---|---|
| REST `GET /accounts/{a}/ai-gateway/gateways` | gateway list incl. `collect_logs`, log retention cap (`log_management`, e.g. 100000) and strategy (`DELETE_OLDEST`) |
| REST `GET .../gateways/{g}/logs` | page-based (`page`, `per_page`, `result_info.total_count`), `order_by=created_at`, `order_by_direction=asc|desc`. Row fields: `id` (ULID), `created_at`, `event_id`, `provider`, `model`, `model_type`, `path`, `duration`, `request_type`, `status_code`, `success`, `cached`, `tokens_in`, `tokens_out`, `usage_metadata{input_tokens,output_tokens,total_tokens,output_reasoning_tokens,input_cached_tokens}`, `timings{total,latency}`, `location{region,colo}`, `cost`, `custom_cost`, `metadata`, `step`, `feedback`, `score`, `prompts`, `guardrails`, `authentication`, `wholesale`, `byok`, `user_agent`, `dlp_action`, `dlp_profiles`. `request`/`response` are **empty strings in the list** |
| REST `GET .../logs/{id}` | adds `request_head`, `response_head`, `*_head_complete`, `request_size`, `response_size`, `request_content_type` |
| REST `GET .../logs/{id}/request`, `/response` | the full raw bodies (prompt / completion JSON). One extra call per body |
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
7. **`aiGatewayRequestsAdaptiveGroups` returned zero rows for a 6-day window** while the REST log
   listed a request from minutes earlier. Unresolved: reconcile before trusting GraphQL for AI Gateway
   metrics; the REST log is the verified source.
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
