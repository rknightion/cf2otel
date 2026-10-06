---
id: CFO-0051
title: >-
  Operational parity: discovery, filtering, error taxonomy, rate limiting,
  multi-account, pull endpoint
status: Done
assignee: []
created_date: '2026-09-30 21:17'
updated_date: '2026-10-06 10:08'
labels:
  - parity
  - ops
dependencies: []
priority: medium
type: feature
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operational features other exporters offer that cf2otel lacks. Parent of the ops subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All subtasks are Done or archived by owner decision
- [x] #2 doc-0004 operations section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop26: reconcile operations parity against Done subtasks and the explicit owner decision to archive multi-account scope; update the parity document, verify CLI readbacks, and run just check before review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop26 CLI readbacks confirm five remaining operations subtasks are Done and every Operations row is resolved; multi-account support was archived under the explicit owner decision, not delivered.

Loop26 completion: R1 attempts none; tracker reconciliation completed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Loop26: archived CFO-0051.05 (multi-account support) under owner decision 2026-10-06; cf2otel remains single-account, one instance per account. Reconciled doc-0004 Operations with the explicit won’t-do disposition and five Done subtasks; CLI readbacks prove both parent acceptance criteria. No new live telemetry proof claimed.
<!-- SECTION:FINAL_SUMMARY:END -->
