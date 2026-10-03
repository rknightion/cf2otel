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
| `prometheus` | Optional `enabled` (default `false`) and `listen` (default `127.0.0.1:9465`) for unauthenticated `/metrics` alongside OTLP. |
| `state` | Persistent checkpoint directory; default `/var/lib/cf2otel`. |
| `health`, `log` | Loopback health listener and application logging. |
| `firewall` | `rule_dimensions` defaults to false; `max_metric_series_per_window` is a positive total cap, default 500. |

Collector keys are `access.logins`, `access.login_metrics`, `access.scim`, `access.seats`, `inventory.access`, `httpreq.events`, `httpreq.metrics`, `aigateway.logs`, `aigateway.metrics`, `aigateway.coverage`, `audit.logs`, `firewall.events`, `firewall.metrics`, `dns.events`, `dns.metrics`, `rum.pageloads`, `rum.web_vitals`, `gateway.dns`, `workers.overview`, `workers.invocations`, `turnstile.events`, `logpush.health`, `d1.analytics`, `d1.queries`, `d1.storage`, `kv.operations`, `kv.storage`, `r2.bandwidth`, `r2.catalog_data`, `r2.catalog_maintenance`, `r2.operations`, `r2.storage`, `r2.sql`, `durableobjects.invocations`, `durableobjects.periodic`, `durableobjects.sql_storage`, `durableobjects.subrequests`, `queues.backlog`, `queues.consumer`, `queues.delayed_backlog`, `queues.message_operations`, `email.routing`, `email.sending`, `selfobs`, `certs.packs`, and `tunnels.status`. Enabled collectors default to five-minute intervals. `access.seats` is enabled by default and polls every 15 minutes as a snapshot, without windows or checkpoints. It counts the independent `access_seat` and `gateway_seat` user flags across the complete paginated Access users list, emitting only the bounded seat-type attribute. Both flags may be true for one user; do not sum the two series as a unique billing-user total. An empty list publishes two zeros; missing, null or nonboolean flags, HTTP errors and incomplete pagination fail the scrape without publishing partial counts. To disable it, set `CF2OTEL_COLLECTORS__ACCESS_SEATS__ENABLED=false`; to override its interval, set `CF2OTEL_COLLECTORS__ACCESS_SEATS__INTERVAL=30m`. The email collectors sum Groups counts across account-owned zones over complete five-minute buckets; they emit no zone metric attributes. DMARC is excluded. `aigateway.metrics` is disabled and unscheduled because its GraphQL Groups ingestion lag is not bounded; `aigateway.logs` emits the AI Gateway metrics from REST rows. The default initial lookback is 30 minutes and maximum window is one hour. `aigateway.coverage` is present but disabled by default. The scheduler advances a checkpoint after a successful window or after it drops a window following three payload rejections.

The Access REST log has only about a day's observed reach. Keep its polling interval short and preserve the state directory; a long outage cannot be repaired by expanding the lookback. Cloudflare GraphQL retention and permitted window width vary by dataset and plan, so cf2otel negotiates available fields and splits requests to fit reported limits.

`httpRequestsAdaptive` event rows are sampled. Use the companion `httpreq.metrics` collector for corrected aggregate counts; do not count event rows to calculate a request rate.

See [Security and PII](security.md) before enabling AI Gateway body capture or wider HTTP scope.

## Workers AI metrics

`workersai.metrics` is disabled by default. Enable it with
`CF2OTEL_COLLECTORS__WORKERSAI_METRICS__ENABLED=true` or its collector YAML entry.
Defaults are `interval: 5m`, `initial_lookback: 30m`, and `max_window: 1h`.
An enabled collector requires `max_window` of at least `5m`, matching its complete
source buckets; smaller windows are rejected during configuration validation.
It uses the configured account and `aiInferenceAdaptiveGroups`, emitting only
account aggregate inference count, input/output tokens and total inference time.
There is no new config group; `platform.max_metric_series_per_window` (default
500) caps its aggregate series. It does not select model IDs or raw inferences.

Queries cover complete UTC five-minute half-open buckets. The scheduler applies
a ten-minute operational holdback and bucket alignment, not a verified bound on
ingestion lag. Late arrivals in already committed buckets are not recovered.
Unavailable or null optional sums are omitted without fabricated zeros. Dataset
entitlement, field, page, duration and retention limits are checked; a saturated
single bucket or invalid numeric value fails atomically. Preserve checkpoints to
avoid recounting previously successful windows.

## Source metric and attribute deny-lists

`otlp.metric_denylist` and `otlp.attribute_denylist` default to `[]`. Each entry
must exactly match a declared `internal/semconv` metric name or attribute key,
respectively. Use OTLP names before Prometheus translation, not underscores,
renames, wildcards or regular expressions. Unknown, empty and wrong-case members
fail configuration loading before any exporter or Cloudflare initialization;
duplicates are deduplicated. Environment overrides are comma-separated:
`CF2OTEL_OTLP__METRIC_DENYLIST` and `CF2OTEL_OTLP__ATTRIBUTE_DENYLIST`. An empty
environment value clears the list.

```yaml
otlp:
  metric_denylist: [cloudflare.dns.queries]
  attribute_denylist: [cloudflare.access.user.id]
```

Denied metrics are not registered or observed by the SDK, including the direct
`cf2otel.metric.cardinality_overflows` counter. Attribute filtering covers metrics,
logs, spans, span events, correlated logs and span links, including generated
`event_name` and the overflow counter's `cf2otel.instrument`. Resource attributes
such as `service.name` are unchanged, even when their key is configured; SDK-added
attributes such as the overflow marker are outside this semantic-key policy.
Filtering does not remove log/span names or rewrite bodies, stop source reads,
change checkpoints, or remove data already exported. Denying `event_name` removes
the usual Loki `| event_name="..."` query selector from new logs; the log body and
signal still remain.

Denying `cloudflare.access.identity.inferred` also removes Access user email,
user ID, user IP address and inference login-ray reference from any attribute
bag originally marked as inferred. This prevents inferred identity from looking
like direct identity. Unmarked or explicitly non-inferred identity follows only
its own configured denies. The denied marker is never retained implicitly, and
this rule applies independently to nested span events, logs and links.

Dropping a metric dimension loses that distinction. Counters and histograms
aggregate into the remaining series. Gauges, including retained snapshots, use
the **last input point** on a collision, never a sum or average. Snapshot filtering
happens before retention, preserves paired atomic publication, and does not
refresh stale TTLs. A dry-run counts only measurements that survive filtering.
These settings are startup-only; no reload is supported.

## Optional Prometheus pull endpoint

Set `prometheus.enabled: true` or `CF2OTEL_PROMETHEUS__ENABLED=true` to serve
`/metrics` in scheduled, non-dry-run mode. `--once`, bounded time-range runs,
exploration, validation and effective-config printing do not start the listener or
register its reader. `CF2OTEL_PROMETHEUS__LISTEN` overrides the YAML address;
use a host and numeric port from 1 to 65535. The default is `127.0.0.1:9465`,
separate from health. An occupied port fails startup rather than selecting another.

OTLP configuration is still required. The same SDK instruments and snapshot
callbacks feed both readers; scraping does not poll Cloudflare, trigger an OTLP
flush or change logs and traces. Prometheus names escape dots to underscores and
add standard unit and counter suffixes (for example, `cf2otel.api.requests` becomes
`cf2otel_api_requests_total`). OTLP names stay unchanged. The private registry does
not add Go/process metrics, `target_info`, scope metadata or resource labels.
Configure scrape job/instance identity in the scraper. The exporter dependency is
experimental (`go.opentelemetry.io/otel/exporters/prometheus` v0.68.0).

The endpoint has **no authentication or TLS** and exposes existing signal attributes,
which may be sensitive. Restrict access with network policy or an authenticated
reverse proxy before explicitly configuring a non-loopback address such as `:9465`.
No deployment or network opening is implied by these examples.

Helm renders the settings through `config.prometheus`; for pod-IP scraping use
`--set config.prometheus.enabled=true --set-string config.prometheus.listen=:9465`.
The chart creates no metrics Service and does not publish a port by default.
For compose, explicitly set `CF2OTEL_PROMETHEUS__ENABLED=true` and
`CF2OTEL_PROMETHEUS__LISTEN=:9465` in the environment; both variables have named
entries in `deploy/docker-compose.yaml`. Only if host access is needed, uncomment
the example port mapping after restricting access. The default remains disabled
with no published port.

## Firewall metric dimensions

Set `firewall.rule_dimensions: true` (or `CF2OTEL_FIREWALL__RULE_DIMENSIONS=true`) to replace the default metric family's dimension set with advertised rule ID, host and client country dimensions. Optional dimensions are selected in rule ID, host, then country order within the dataset's advertised field budget. Saturated source windows are bisected down to one minute before aggregation; an incomplete leaf fails the window without emitting partial counts. It does not emit a second copy of each event. Rule descriptions are read from custom and managed phase zone rulesets and cached per zone for one hour; denied or failed lookups leave descriptions absent without failing metrics. Free ByTimeGroups remains count-only because its schema rejects dimensions despite advertising them.

`firewall.max_metric_series_per_window` (`CF2OTEL_FIREWALL__MAX_METRIC_SERIES_PER_WINDOW`) bounds all emitted firewall points across zones, including a reserved remainder slot when the window exceeds the cap. Identical series aggregate first. Legacy low-cardinality points take priority, then enrichment points in deterministic attribute order. Discarded counts go to one all-other series (zone, action, source, rule ID, description, host and country all `other`). At cap 1 it holds the entire count. Counts are conserved, never duplicated between base and enrichment. This collector-side per-window cap is independent of HTTP/platform caps and the SDK cardinality limit; cumulative SDK series can still grow across windows.

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
| `warp.last_seen_window` | `15m` | Positive, at most `60m`; bounded `last_seen` query for recently seen devices, not all physical devices. Environment: `CF2OTEL_WARP__LAST_SEEN_WINDOW`. |
| `warp.max_metric_series` | `500` | `1..5000` normal full-dimension tuples plus at most one count-preserving remainder. Environment: `CF2OTEL_WARP__MAX_METRIC_SERIES`. |
| `collectors.warp.fleet` | Disabled, `5m` interval | Account-level doc-derived device snapshot; no checkpoint/window buffering. Enable with `CF2OTEL_COLLECTORS__WARP_FLEET__ENABLED=true`; interval via `CF2OTEL_COLLECTORS__WARP_FLEET__INTERVAL`. Snapshot expires after three actual polling intervals, failed polls do not refresh it, and genuine empty lists clear it. |
| `collectors.tunnels.status` | Disabled, `1m` interval | Requires Cloudflare Tunnel Read. Without permission the API returns an empty 200, not a 403. Snapshot collector with no checkpoint key; first poll after startup emits no status-change event. (CFO-0048.01) |

Override these keys using `CF2OTEL_OTLP__METRIC_CARDINALITY_LIMIT`,
`CF2OTEL_HTTP__REQUEST_SOURCE` and `CF2OTEL_HTTP__BREAKDOWNS`. The breakdown environment
value is comma-separated (for example `country,method`); an empty value disables all breakdowns.
Collector environment forms are `WORKERS_INVOCATIONS`, `CERTS_PACKS` and `TUNNELS_STATUS`.
Snapshot collectors use the polling interval, not the window or initial lookback.

## DEX test result snapshots

`dex.tests` is disabled by default and polls every `5m` when enabled. Enable it with
`CF2OTEL_COLLECTORS__DEX_TESTS__ENABLED=true`; set its polling interval with
`CF2OTEL_COLLECTORS__DEX_TESTS__INTERVAL`. It reads current provider averages, not
historical windows, and has no checkpoint or backfill.

| Key | Default | Contract |
| --- | --- | --- |
| `dex.result_window` | `1h` | `1h..168h` inclusive; current UTC result window, ISO timestamps with milliseconds and `interval=minute`. Environment: `CF2OTEL_DEX__RESULT_WINDOW`. |
| `dex.max_tests` | `1000` | `1..10000`; complete overview enumeration with `page`/`per_page=50`. Exceeding the bound or repeating a test/page fails without refreshing the prior snapshot. Environment: `CF2OTEL_DEX__MAX_TESTS`. |
| `dex.max_metric_series` | `500` | `6..5000` total metric/name/kind identities across polls until restart. Six slots are reserved for the two HTTP and four traceroute signal/kind remainders. Environment: `CF2OTEL_DEX__MAX_METRIC_SERIES`. |

Named identities are admitted in lexical test-name/kind/metric order, then remain
sticky until restart, even when temporarily absent. New or unsafe names overflow
to `other`, separately for each signal and kind. At cap `6` all output uses
remainders; at cap `500` there is room for at most `494` named identities. This is
not a test count cap: an HTTP test can contribute two identities and a traceroute
test four. Remainders are arithmetic means of per-test provider averages, **not**
sample- or device-weighted aggregates. Missing/null optional averages are omitted,
not zero-filled. Review test names for sensitive content before opting in; IDs
never become labels. See [DEX signal semantics](signals.md#dex-test-results).

Each test is fetched independently through the shared retrying client. Failed
tests do not block valid tests: their successful subset replaces all five metric
families atomically and the poll still reports an error. If all detail requests
fail, prior data retains its original expiry, three actual polling intervals.
A genuinely empty complete catalog clears data; catalog failure does not. Names
that collide for distinct test IDs of the same kind fail the catalog, without an
ID fallback. Detail endpoints are documented-only, explicitly reported unprobed
by the drift canary; only the overview is live-probed. No populated DEX result or
runtime behavior has been live-verified.

## Grafana rule-generation interval alignment

`GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS` is a generator-only positive integer in seconds,
defaulting to `60`. It is not a runtime `CF2OTEL_` environment variable and does not configure
collector polling. Its value **must match the deployed `collectors.tunnels.status.interval`**
when generating Grafana alerts. Invalid or nonpositive values stop generation before any rule
is written.

The tunnel-unhealthy alert requires collector last-success age strictly less than three times
this interval. For a deployment polling every `5m`, generate its manifests with:

```sh
GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS=300 just gen </dev/null
GRAFANA_TUNNELS_STATUS_INTERVAL_SECONDS=300 just gen-check </dev/null
```

Regenerate and provision the matching rules whenever the deployment interval changes. The
checked-in rules use the default `60` seconds; use default generation for repository baseline
checks, and generate deployment-specific artifacts in a separate staging checkout. An interval
that is too short can reset the five-minute pending period between successful polls; one that is
too long can treat stale snapshots as current. Stale or never-successful collectors do not establish
health, and this setting does not enable the disabled-by-default collector.

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

### Metric HTTP partial-success reporting

An offline reproduction through `NewProviders`, metric emission, `Shutdown` and the CLI's
self-observability callback verifies the pinned `otlpmetrichttp` v1.46.0 contract. The fake
OTLP endpoint decodes an actual protobuf request containing at least two datapoints and returns
HTTP 200 with `Content-Type: application/x-protobuf`. A full-acceptance response produces no
caller or observer error and increments `cf2otel.export.success` for `signal=metrics`. A valid
`partial_success` response with `rejected_data_points=1` and an error message produces an error
at both boundaries and increments `cf2otel.export.errors`, not the success counter. cf2otel
omits the backend message from that error. `TestMetricHTTPPartialSuccessOutcome` pins these
outcomes; the silent-acceptance hypothesis failed by assertion on the unchanged pinned SDK.
No SDK defect or dependency correction was demonstrated for this response contract.

HTTP 200 alone does not prove that all datapoints were accepted. Nor does an absent diagnostic
prove acceptance of individual datapoints: these counters describe export outcomes, not backend
storage or per-datapoint receipts. Metrics are exported by a periodic reader outside window
commits; their failures do not prevent log/span checkpoint advancement, and this reproduction
does not establish metric replay or at-least-once metric delivery. The verified result is limited
to protobuf metric HTTP responses, not other encodings, protocols or signals.

## Opt-in high-cardinality HTTP breakdowns

Add `colo`, `asn` and/or `error_path` to `http.breakdowns`. None is in the default list;
existing low-cardinality breakdowns are unchanged. These counters use adaptive **Groups counts**,
not event-row counts, and follow `http.request_source`. Each zone selects a feature only when
all its required fields fit the advertised field budget. ASN requires both string `clientAsn`
and `clientASNDescription`; current live verification found both on one eligible zone, not on
all Free zones (API reference section 18). Missing entitlement skips that feature, not base metrics.

| Key | Default | Contract |
| --- | --- | --- |
| `http.high_cardinality_limit` | `500` | Integer `1..5000` normal groups per new metric and zone, plus at most one count-preserving remainder. |
| `http.high_cardinality_hosts` | `[]` | At most 50 lowercase bare ASCII DNS names. Exact case-insensitive source-host match; no ports, URLs or wildcards. Only the three new features are filtered; existing `http.hosts`, base totals and low-cardinality metrics are unchanged. Hosts are never added to these new metric labels. |
| `http.error_path_routes` | unset | Safe configured route name to canonical normalized path template. Required and nonempty when `error_path` is enabled; at most 5000 unique templates. |

Environment keys are `CF2OTEL_HTTP__HIGH_CARDINALITY_LIMIT`,
`CF2OTEL_HTTP__HIGH_CARDINALITY_HOSTS` (comma-separated; empty clears it),
and nested `CF2OTEL_HTTP__ERROR_PATH_ROUTES__<NAME>` entries. Route names must match
`^[A-Za-z][A-Za-z_-]{0,63}$`; `other`, `unknown` and all-hex names of eight or more characters
are reserved/rejected. Names cannot be paths or identifiers. For example:

```yaml
http:
  breakdowns: [colo, asn, error_path]
  error_path_routes:
    item: /items/:number
```

Templates are absolute paths, already normalized, with only `:number`, `:uuid` and `:hex`
placeholders. Raw numeric/UUID/hex segments, query strings, fragments, percent escapes,
URLs, dot segments, wildcards and regex syntax are rejected in config. Duplicate templates
are an error, never first-map-entry wins. Runtime paths have query/fragment removed and
are percent-decoded once as valid UTF-8, then full numeric, UUID and hex (eight or more
ASCII hex characters) segments normalize internally. Exact template matches yield only the
configured safe **name**, never the raw or normalized path. Unmatched/invalid/nonabsolute
paths and paths longer than 4096 bytes contribute to the remainder.

Counts are summed across disjoint duration periods and repeated groups before emitting.
Normal tuples are kept in lexical order up to the per-feature limit; all unmatched routes
and excess tuples become one all-other tuple with remainder `true`. Normal points have
remainder `false`. Error routes count only integer statuses 400..599, with `4xx`/`5xx` classes;
the remainder's status class is `other`. Host-filtered counts are excluded, not put in the remainder.

These points still count toward the unchanged **total** `http.max_metric_series_per_window`.
With all three enabled at limit 500, a zone can add up to 1503 points; a large fleet can
legitimately fail the total budget. Source/shape/cancellation failures, source arrays or
assembled feature windows reaching 10000 rows, count overflow and a total-budget breach
fail the complete window before metric publication or checkpoint advance. Source paging
is not assumed complete at a saturated limit. The SDK's cumulative lifetime cardinality
limit is independent; a per-window bound does not cap series accumulated across windows.

## Load balancer health snapshots

`loadbalancers.health` is disabled by default, with interval `5m`. Opt in using
`CF2OTEL_COLLECTORS__LOADBALANCERS_HEALTH__ENABLED=true`; change the interval with
`CF2OTEL_COLLECTORS__LOADBALANCERS_HEALTH__INTERVAL`. It is a snapshot collector,
not a window collector, and needs no new configuration group or checkpoint.

It reuses `platform.max_metric_series_per_window` (default 500) as the cap for
this single gauge, reserving one `other` slot, leaving 499 sticky named identities
at the default. A cap of 1 permits only `other`; admission persists until restart.
The configured names must be nonempty, at most 128 UTF-8 bytes without controls;
ambiguous duplicate names or IDs fail the whole catalog without publication.
The unexported catalog bound is 1000: reaching it fails closed, never silently
truncates. The documented pool-list GET has no pagination query; the collector
adds none. See [health semantics](signals.md#load-balancer-provider-health-flag).

Snapshots expire after three configured intervals. Successful valid subsets
replace prior values atomically; failed-only fetches retain the original expiry.
An all-unknown valid detail response publishes no measurements and returns an
unknown-coverage error, so stale known health is not refreshed as current health.
An empty valid pool list is an error-free clear with no detail requests. Detail
shapes are doc-derived and fixture-tested only; do not interpret this signal as
regional pool availability or complete fleet coverage.
