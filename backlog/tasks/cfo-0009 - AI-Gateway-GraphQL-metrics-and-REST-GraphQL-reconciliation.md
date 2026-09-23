---
id: CFO-0009
title: AI Gateway GraphQL metrics and REST/GraphQL reconciliation
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:14'
labels:
  - 'wave:1'
  - aigw
dependencies: []
priority: medium
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
aiGatewayRequestsAdaptiveGroups, aiGatewayErrorsAdaptiveGroups, aiGatewayCacheAdaptiveGroups. doc-0003 trap 7: the Groups dataset returned zero rows while REST had fresh logs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause of trap 7 is established and recorded in doc-0003 (or the dataset is dropped with the evidence)
- [ ] #2 If used, GraphQL-derived metrics agree with REST-derived counts within the sampling tolerance over a measured window
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
W8 read-only reconciliation on 2026-09-23: identical 3h GraphQL Groups window initially returned 0 with 38 REST requests (latest 13.9m old), then later returned 38; other 1h and 3h windows matched 2/2 and 39/39. Unknown upper lag. Dropped GraphQL collector from registration; REST supplies bounded metrics. Corrected doc-0003 trap 7 via Backlog CLI.

Parked: GraphQL AI Gateway Groups changed from zero to matching REST counts for an unchanged 3h window after unbounded ingestion lag. REST metrics are enabled and GraphQL collector disabled. Resume only with measured upper lag and a reconciliation tolerance; AC2 is conditional and not exercised.
<!-- SECTION:NOTES:END -->
