---
id: CFO-0045
title: >-
  DNS analytics metric overflows the OTel SDK cardinality cap; make the cap
  configurable
status: To Do
assignee: []
created_date: '2026-09-30 21:15'
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
- [ ] #1 A reproduction test shows the overflow before the fix and none after
- [ ] #2 The DNS metric carries no attribute combination that can exceed its cap at default config; colo stays available on dns.query log events
- [ ] #3 A documented config key sets the SDK cardinality limit (default unchanged unless justified), validated at load
- [ ] #4 Any instrument reaching its cardinality limit is visible in cf2otel self-observability (metric or warning log naming the instrument), not only in the backend
- [ ] #5 After deploy, the live DNS metric has no otel_metric_overflow series over 24h
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
