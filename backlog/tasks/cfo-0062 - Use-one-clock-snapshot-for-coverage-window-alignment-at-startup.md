---
id: CFO-0062
title: Use one clock snapshot for coverage window alignment at startup
status: Done
assignee:
  - loop14-root
created_date: '2026-10-01 06:46'
updated_date: '2026-10-01 07:32'
labels:
  - aigateway
dependencies: []
priority: high
type: bug
ordinal: 86000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 deploy health rejected the release after aigateway.coverage reported a range without a closed five-minute window at startup. The scheduler and coverage Lag independently read the clock, so a boundary can be computed from inconsistent instants. Preserve closed-window and holdback safety while reproducing the observed startup error before repair.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The startup range reproduces the closed-window error before the fix and completes safely afterward
- [x] #2 Coverage end alignment and ten-minute holdback use a consistent clock snapshot without accepting partial windows
- [x] #3 Existing scheduler delivery and coverage fail-closed range tests remain passing
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Trace the recorded startup failure and prove the cause through the registered scheduler path; repair only clock/window alignment, preserve all unsafe-range rejection checks, then run the local gate and CodeRabbit followed by independent review.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Optional WindowLagAt uses one scheduler clock snapshot while retaining the existing WindowCollector and Lag fallback. Registered scheduler cold/resumed startup reproduces the exact closed-window error on base, then passes six boundary cases on candidate 316601492bca4d2c826a07beab62819f46bc4e22. Full gates, CodeRabbit and independent exact-SHA review passed; landing e58a01c036e6476d7b61bf71048653ee90717fcd passed CI 36828474549. Explicit-range, retention, multiple-window, holdback and delivery checks remain unchanged. This fixes the reproduced cause; healthy deployed proof is still pending and historical exclusive causality is not asserted.
<!-- SECTION:FINAL_SUMMARY:END -->
