---
id: CFO-0022
title: HTTP metrics for all zones
status: Parked
assignee:
  - '@rob'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 08:23'
labels:
  - 'wave:2'
  - http
dependencies: []
priority: low
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend httpRequestsAdaptiveGroups metrics beyond Access-protected hosts to every zone, with cardinality limits.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Configurable zone scope with cardinality thresholds
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Add a separate metrics scope that inherits the existing HTTP scope by default, with configurable per-zone host and per-window series limits. Implement all-zone Groups metrics without expanding per-request events, then verify limits and live signal.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Prebuild discovery: account zone listing succeeded and a bounded httpRequestsAdaptiveGroups sample returned nonzero count rows. Sample payload is private and excluded from the tracker.

Loop 3 source at 20e9981 adds http.metrics_scope (empty inherits the prior HTTP scope), a 1000-host per-zone cap and 10000-series per-window cap. All-zone metrics remain separate from request-event scope; discovered zones are restricted to the configured account before explicit selectors; cap or incomplete-zone failures leave the checkpoint unchanged. Prechange regressions failed; exact-SHA just check, independent REV and CodeRabbit completed. Read-only local harness queried 23 configured-account zones in [07:34,07:49) UTC, emitted 31 request points totaling 3895 and four optional-duration points; 33 provider -1 duration sentinels were omitted from gauges while requests were retained. No Camden config change or deployed all-zone OTLP/m7kni signal occurred. Resume by releasing/deploying source, explicitly selecting metrics_scope=all in approved Camden config, then verify checkpoint and m7kni request counts without broadening events. Keep task Parked until that live proof.
<!-- SECTION:NOTES:END -->
