---
id: CFO-0022
title: HTTP metrics for all zones
status: In Progress
assignee:
  - '@rob'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 07:15'
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
- [ ] #1 Configurable zone scope with cardinality thresholds
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Add a separate metrics scope that inherits the existing HTTP scope by default, with configurable per-zone host and per-window series limits. Implement all-zone Groups metrics without expanding per-request events, then verify limits and live signal.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Prebuild discovery: account zone listing succeeded and a bounded httpRequestsAdaptiveGroups sample returned nonzero count rows. Sample payload is private and excluded from the tracker.
<!-- SECTION:NOTES:END -->
