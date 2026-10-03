---
id: CFO-0074
title: Validate minimum windows for bucket-aligned collectors
status: In Progress
assignee:
  - '@loop17-root'
created_date: '2026-10-02 15:55'
updated_date: '2026-10-03 12:17'
labels: []
dependencies: []
priority: low
type: bug
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 independent source review found that the opt-in Workers AI collector requires a complete five-minute source bucket while configuration validation accepts any positive max_window. A shorter configured window can repeatedly fail without advancing. Defaults are unaffected; runtime reproduction is still required.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A public configuration and scheduler reproduction demonstrates the sub-bucket window failure by assertion before correction.
- [ ] #2 Incompatible enabled collector window settings are rejected early or have a documented supported treatment, without weakening existing source-window and checkpoint guarantees.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop17 frozen packet: one local implementation attempt with assertion-red witness, just check and CodeRabbit to terminal result; fresh exact-SHA independent REV before root linear landing. No lane remote writes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 fresh REV-FIX-74 FAIL 8f0146f8e0dd209fe1823459929a0e3aaef9d792: documented max_window5m with public initial_lookback31m accepts an unaligned startup cursor and permanently stalls without a complete source bucket. Root RR1 of3 chooses conservative minimum two source buckets (10m) for enabled Workers AI existing max_window, rather than changing shared scheduler checkpoint semantics. Preserve full-bucket guarantees, pin public startup and repeated cycle/restart with real HTTP regression. Explicit narrow owned-test exception permits existing workersai/integration_test.go for required public boundary witness; no other collector source ownership granted.
<!-- SECTION:NOTES:END -->
