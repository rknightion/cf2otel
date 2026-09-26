---
id: CFO-0038
title: AI Gateway log-coverage check from GraphQL Groups counts
status: To Do
assignee: []
created_date: '2026-09-26 16:05'
updated_date: '2026-09-26 18:34'
labels:
  - ai-gateway
dependencies: []
priority: low
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
REST gateway logs are the primary AI Gateway source. Requests sent with log collection off, or dropped by gateway log limits or retention, never appear in REST. aiGatewayRequestsAdaptiveGroups counts them anyway. Emit a coverage signal comparing the GraphQL Groups request count with the REST-derived request count per gateway over windows held back past ingestion lag (loop 3 measured 139-365 s event-to-Groups; hold back at least 10 minutes). It is a completeness check only, never a second primary rate. Decided by Rob 2026-09-26 when closing CFO-0009.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A metric declared in internal/semconv reports GraphQL Groups minus REST request count per gateway for each lag-safe window, with a test covering equal, missing-logs and GraphQL-behind cases
- [x] #2 Selections come from settings.availableFields; the collector respects maxDuration and notOlderThan and never double-counts a window on replay
- [ ] #3 Live: after a Camden deploy, one window's GraphQL count, REST count and the emitted value agree on m7kni Mimir
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 8: PROBE-38 (read-only, [16:40Z,16:45Z)): REST logs list with start_date/end_date RFC3339 returned 200 and result_info.total_count 19 = paged rows; Groups aiGatewayRequestsAdaptiveGroups count for the gateway 19; dimensions_gateway advertised. L38 de07aa7 added collector aigateway.coverage (gauge cloudflare.ai_gateway.log_coverage.gap {request} per gateway; Groups count minus distinct REST ids for one closed five-minute window at least 10 minutes old; selection checked against availableFields; maxDuration/notOlderThan enforced; retry recomputes the same window). Root added the aiGatewayRequestsAdaptiveGroups API-contract entry (db2875b). REV-L38 round 1 FAIL (interval over 5m lost windows; jitter false scrape errors; -since/-before skipped the holdback; no dedupe test) -> review-repair round 1 b01ed38 (cadence pinned to 5m, boundary-aligned Lag, holdback refusal, dedupe test) -> REV-L38 round 2 PASS. Landed in 8a56011d3520a647458d221fc5d482715a897ba2; CI 36261997229 success incl ci-success after an infrastructure rerun (a fleet Actions allowlist change briefly blocked nested actions). Off by default; enable only in YAML under collectors: aigateway.coverage with enabled, interval, initial_lookback and max_window (the env form fails for dotted names, CFO-0039). AC3 open: live proof needs the collector enabled on camden, which needs a config.yaml edit this loop may not make.
<!-- SECTION:NOTES:END -->
