---
id: doc-0004
title: Parity checklist - reference pollers
type: specification
created_date: '2026-09-23 09:59'
updated_date: '2026-10-02 14:17'
---
cf2otel must cover every capability listed here. The list is a behaviour contract drawn from a survey
of open-source Cloudflare pollers and exporters; no other project is named and no code is copied.
Output is OTLP (logs, metrics, traces), with an optional Prometheus pull endpoint (CFO-0051.06): an
equivalent surface is required, not the same wire format. Where sources disagree, the stronger
behaviour is the target.

Tags: **[Go]** and **[Py]** mark the two log-shipping pollers surveyed first (2026-09-23); **[Prom]**
marks the Prometheus-style exporter survey (2026-09-30). The later items name the Backlog task that
delivers them.

Cardinality policy (Rob, 2026-09-30): low-cardinality dimensions are on by default, high-cardinality
ones (colo, ASN, path, per-device, per-user) are opt-in, and every metric stays under a series cap.
The OTel SDK cardinality limit is per instrument and configurable (CFO-0045).

## Access / Zero Trust logins

- [Py] Poll `GET /accounts/{a}/access/logs/access_requests` with a persisted, event-driven cursor.
- [Py] Fields: action, email, IP, app domain, country, allowed, ray ID, created-at.
- cf2otel adds: GraphQL `cf1AccessLoginsRawGroups` / `accessLoginRequestsAdaptiveGroups` metrics,
  SCIM update log, app and user inventory, service-token (`nonidentity`) separation.

## Account audit logs (wave 2)

- [Both] Poll `/accounts/{a}/logs/audit` (v2) with cursor pagination via `result_info.cursor`.
- [Go] Dedupe the `since` boundary by event ID (inclusivity is undocumented). Target: this, not the
  Python `last + 1s` cursor, which can drop same-second events.
- [Py] Per-cycle page cap as a safety valve.
- [Both] Actor (id, email, IP, token id/name, type), action (type, time, description, result),
  resource (type, id, product), raw request (ray, method, status, URI, UA).

## Firewall / WAF events (wave 2)

- [Both] Raw `firewallEventsAdaptive` (not the Groups aggregate), exclusive lower-bound cursor.
- Target field set is the [Py] superset: action, source, kind, client IP / country / ASN / ASN
  description, host, method, path, query, protocol, edge and origin status, WAF attack score class,
  rule ID, ruleset ID, ray, colo, UA - each subject to `availableFields`.
- [Go] Warn when a page hits the limit; [Py] advance the cursor mid-run and loop, with a page cap.
- [Py] Severity mapping from action (block 7, challenge family 6, log 3, allow/skip 2, default 4) -
  cf2otel maps this to OTel `SeverityNumber`.

## HTTP traffic

- [Go] Aggregated `httpRequestsAdaptiveGroups` into cumulative counters (method, status, country,
  zone) plus edge bytes. Non-overlapping windows so `Add` never double counts.
- [Py] Raw per-request `httpRequestsAdaptive` events with cache status, WAF class, client IP/ASN, UA.
  CEF severity 8 for 5xx, 5 for 4xx, else 3.
- Target: both. Groups feed metrics; raw events feed logs.

## DNS analytics (wave 2)

- [Py] Raw `dnsAnalyticsAdaptive`: query name, type, response code, cached, protocol, colo.

## Web Analytics / RUM (later wave)

- [Go] `rumPageloadEventsAdaptiveGroups` counters (page views, sessions by country and device; path
  and referrer never in metrics).
- [Go] Hold the leading edge back 10 minutes for ingestion lag, or data is skipped permanently.
- [Go] `rumWebVitalsEventsAdaptiveGroups` p75 LCP/INP/FID/FCP/TTFB/CLS by device, re-queried over a
  rolling window, `-1` sentinel means no data, stale series cleared before repopulating.

## Transport, retry and limits

- [Go] Retry 429/502/503/504 with `Retry-After`, else exponential backoff, bounded attempts.
- [Go] Cap response size (10 MB) and set client timeouts (30 s).
- [Py] Clamp the poll interval to a floor (30 s) so a zero or negative value cannot hot-loop.
- [Py] Per-source error isolation: one failing dataset never stops the others.
- [Py] Hold GraphQL windows to completed intervals (`until = now - 1m`).

## State and cursors

- [Py] Persist cursors across restarts, atomically (temp file in the **same directory**, then
  rename, to avoid cross-filesystem `EXDEV`). [Go] keeps them in memory only - a regression to avoid.
- [Go] Initial backfill window when no cursor exists.

## Self-observability and operation

- [Go] Per-dataset poll counters, duration histograms, last-poll timestamps, export outcome counts,
  build info; health endpoint; a span per poll cycle with child client spans; trace-correlated logs.
- [Go] Supervised collectors with panic recovery and restart.
- [Py] CLI: dataset selection, explicit `--since/--before` window, dry run (no send, no state write),
  state reset, verbose per-source counts.
- [Py] Zone auto-discovery via `GET /zones` when no zone list is configured. [Go] requires a list.
- [Py] Read-only exploration mode that prints a dataset as a table/JSON and names the missing
  permission on a 403.
- [Both] Non-root container, healthcheck, hardened compose/systemd.


## HTTP zone analytics [Prom] (CFO-0046)

- [x] Edge response bytes alongside requests; eyeball-only `requestSource` policy (CFO-0046.01).
- [x] Exact edge status, origin status, country, HTTP protocol, TLS protocol, method, content type as
  default dimensions (CFO-0046.02).
- [x] Opt-in colo (host allowlist), ASN, and 4xx/5xx by normalised path (numeric, UUID and hex segments
  collapsed) (CFO-0046.03). Implemented using configured safe route names rather than raw or normalised path labels.
- [x] Edge TTFB average and percentiles; origin duration p50/p95/p99 (CFO-0046.04).
- [x] Visits and threats where the plan allows; account data transfer month-to-date with a linear
  projection (CFO-0046.05).

## Workers and platform depth [Prom] (CFO-0047)

- [x] Workers invocation status, errors, subrequests, CPU/duration/wall-time percentiles per script
  (CFO-0047.01). Raw invocation events stay skipped (CFO-0023).
- [x] Durable Objects errors and wall-time/response-size percentiles with script; D1 rows read/written
  and batch-time percentiles; Queues lag time, retry count and billable operations breakdown
  (CFO-0047.02).
- [x] Logpush failed uploads by job, destination, status and final attempt, account and zone scope
  (CFO-0047.03).
- Bounded per-resource names for D1, KV, Queues and DO; R2 action type (CFO-0047.04).

## Zero Trust state [Prom] (CFO-0048)

- [x] Tunnel status and connector health (CFO-0048.01).
- WARP fleet status as aggregates by status, platform, version, mode and colo, fully paginated; never
  per device (CFO-0048.02).
- DEX HTTP and traceroute test results (CFO-0048.03).
- [x] Access and Gateway seat counts (CFO-0048.04).

## Firewall depth [Prom] (CFO-0049)

- Rule ID and resolved rule description, host and country on the firewall metric (CFO-0049.01).
- Bot score buckets and score source with a fallback for unentitled zones (CFO-0049.02).

## Origin and edge health [Prom] (CFO-0050)

- Certificate pack status and expiry (CFO-0050.01).
- Load balancer pool/origin health by region, origin RTT, pool traffic (CFO-0050.02).
- Health check events with RTT/TTFB/TCP/TLS timings and failure reason (CFO-0050.03).
- Opt-in public status page component status (CFO-0050.04).

## Operations [Prom] (CFO-0051)

- [x] Zone exclude list; discovered/filtered/processed/skipped zone self-metrics (CFO-0051.01).
- Metric and attribute deny list validated against semconv (CFO-0051.02).
- [x] Classified collector errors (CFO-0051.03).
- Per-zone entitlement backoff and a shared client-side rate limiter (CFO-0051.04).
- Multiple accounts, listed or discovered (CFO-0051.05).
- Optional Prometheus `/metrics` endpoint (CFO-0051.06).

## Deliberately out of scope

Magic Transit, Magic Firewall and Network Analytics datasets (Enterprise/Magic products), Stream and
Images statistics, running cf2otel as a Worker, Global API Key authentication, a hostname trace
probe (synthetic monitoring does this better), and a runtime configuration API. The retired REST
analytics endpoints are superseded by the GraphQL datasets above.


Loop 16 source reconciliation at ced4edf: checked items above have landed collector and boundary-test evidence. Checks denote source parity, not fresh deployed proof. WARP connected-device parity, resource names, DEX, deny list, limiter/backoff, multiple accounts and pull endpoint remain unchecked on this baseline.
