---
id: doc-0004
title: Parity checklist - reference pollers
type: specification
created_date: '2026-09-23 09:59'
updated_date: '2026-09-23 10:01'
---
cf2otel must cover every capability of two existing open-source Cloudflare pollers, both MIT:

- **[Go]** `afreidah/cloudflare-log-collector` - Loki push + Prometheus + OTel traces.
- **[Py]** `kozliatko/cf-log-forwarder` - CEF-over-syslog or JSONL, persisted cursor, rich CLI.

Inventory taken 2026-09-23 from a full read of both source trees. Neither project's code is copied;
this list is the behaviour contract. Where the two disagree, the stronger behaviour is the target and
is named. Output is OTLP (logs, metrics, traces) rather than Loki push or CEF: an equivalent surface
is required, not the same wire format.

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
