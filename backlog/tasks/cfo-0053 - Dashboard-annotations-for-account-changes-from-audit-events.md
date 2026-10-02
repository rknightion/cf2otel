---
id: CFO-0053
title: Dashboard annotations for account changes from audit events
status: In Progress
assignee: []
created_date: '2026-09-30 21:17'
updated_date: '2026-10-02 02:40'
labels:
  - dashboard
dependencies: []
priority: low
type: enhancement
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Traffic and error changes are easier to explain next to the configuration change that caused them. Audit events already reach Loki.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The shipped dashboard has a toggleable annotation layer of audit events (actor, action, resource) from Loki
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop15 E53: add one visible toggleable account-change annotation layer from Loki using existing audit event semantic fields. Default disabled to avoid clutter, toggle remains visible. Use official fetched Dashboard v2 AnnotationQueryKind/DataQueryKind shape and Loki log queries, not metric queries. Show actor, action and resource; preserve every existing panel and alert identity. Generated output validation and independent review before root landing and live Grafana readback.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop15 RR1 source candidate independently PASS on exact final commit with complete detached just check, generation/preservation and opaque-fixture proofs. Implementation I1 unchanged; one review-repair consumed, two remain. Accepted locally but not landed or synced yet; annotation import/readback and rendering remain unverified. Root will publish the reviewed full patch as a squash with valid attribution trailers, preserving intermediate source history locally.
<!-- SECTION:NOTES:END -->
