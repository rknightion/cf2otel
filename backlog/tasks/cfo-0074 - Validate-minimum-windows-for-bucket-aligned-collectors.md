---
id: CFO-0074
title: Validate minimum windows for bucket-aligned collectors
status: Done
assignee:
  - '@loop17-root'
created_date: '2026-10-02 15:55'
updated_date: '2026-10-03 12:53'
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
- [x] #1 A public configuration and scheduler reproduction demonstrates the sub-bucket window failure by assertion before correction.
- [x] #2 Incompatible enabled collector window settings are rejected early or have a documented supported treatment, without weakening existing source-window and checkpoint guarantees.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
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

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Enabled Workers AI max_window below10m is rejected with the key named; conservative two-bucket minimum supports unaligned startup and retained cursors without shared scheduler changes. Authentic public YAML/scheduler red on main and5m/6m repair base; supported10m repeated cycles and reopened FileStore pass. Fresh REV-FIX-74-RR1 PASS26173153128b35c36c510cb7a8b3f6cd3cf3c111; unchanged patch landed d6edca7/7982ce7, integrated justcheck/gencheck/scans green and pushed061690dcd59effbbdfe0cc116025361ba8b39488. Landing CI pending exactSHA watcher. No Dockerfile/packaging or new signal changes, so separate image gate and new-name obligations are not applicable.
<!-- SECTION:FINAL_SUMMARY:END -->
