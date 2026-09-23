---
id: CFO-0009
title: AI Gateway GraphQL metrics and REST/GraphQL reconciliation
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
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
- [ ] #1 Root cause of trap 7 is established and recorded in doc-0003 (or the dataset is dropped with the evidence)
- [ ] #2 If used, GraphQL-derived metrics agree with REST-derived counts within the sampling tolerance over a measured window
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
