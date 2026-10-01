---
id: CFO-0045
title: >-
  DNS analytics metric overflows the OTel SDK cardinality cap; make the cap
  configurable
status: In Progress
assignee: []
created_date: '2026-09-30 21:15'
updated_date: '2026-10-01 14:06'
labels:
  - parity
  - dns
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On the live account cloudflare_dns_queries_total has exactly 2000 series and an otel_metric_overflow series: the OTel Go SDK default cardinality limit (2000 datapoints per instrument per collect) is hit, driven by zone x colo x query type x response code x cached x protocol. Past the cap every new attribute set folds into one overflow series, so breakdowns are silently wrong while totals survive. internal/telemetry sets no limit. In SDK v1.46 the limit is per instrument but configured globally (WithCardinalityLimit) or per instrument kind (reader CardinalityLimitSelector); there is no per-metric-name override, so per-metric control stays with cf2otel series caps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A reproduction test shows the overflow before the fix and none after
- [x] #2 The DNS metric carries no attribute combination that can exceed its cap at default config; colo stays available on dns.query log events
- [x] #3 A documented config key sets the SDK cardinality limit (default unchanged unless justified), validated at load
- [x] #4 Any instrument reaching its cardinality limit is visible in cf2otel self-observability (metric or warning log naming the instrument), not only in the backend
- [ ] #5 After deploy, the live DNS metric has no otel_metric_overflow series over 24h
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 exact candidate 6019ce7039e38ef8b5a9eef4926140324103f1e4 passed fresh independent review after the initial default-cardinality guarantee failed review. Registered DNS with real SDK and OTLP export proves the lifetime bound, fallback total preservation and churn behavior; provider limit/default/unlimited and overflow visibility tests pass. Landing d0387dfe728e1893f7a796a2cc80298aaa566528 passed root gate and CI 36826339268. AC5 remains pending deployment plus a full 24-hour window. Dockerfile and packaging unchanged; separate packaging gate not required.

LIVE45 AC5 remains parked, not waived: the healthy DEP2 epoch was 2026-10-01 09:14:17 UTC and a partial read 2.13 hours later reported 131 DNS-family active series, no current SDK-overflow series, and absent cf2otel_metric_cardinality_overflows_total. Those were instantaneous observations, not continuous 24-hour proof. Healthy DEP3 restarted at 2026-10-01 13:37:00 UTC, so a clean post-final-deployment 24-hour window is not yet available. Required final verification: cloudflare_dns_queries_total{service_name="cf2otel",otel_metric_overflow="true"} over the full post-deploy interval must have no series, and cf2otel_metric_cardinality_overflows_total{service_name="cf2otel"} must be absent or flat. Preserve the exact final deploy epoch and query interval; absence at one instant must not be reported as a numeric zero or a full-window pass.
<!-- SECTION:NOTES:END -->
