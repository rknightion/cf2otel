---
id: CFO-0078
title: Diagnose intermittent DNS lifetime-cardinality integration assertion
status: Done
assignee:
  - '@loop18-root'
created_date: '2026-10-03 12:21'
updated_date: '2026-10-04 13:33'
labels:
  - dns
  - testing
dependencies: []
priority: low
type: bug
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop17 P67 local gate at cf7b7f56b0790f0a85147ecf0b42202d55d1997d failed TestRegisteredDNSLifetimeCardinalityBudget/default_many_zones after10.04s: total19642 want20904 points9821 max9999 folded0. Fresh independent exact-SHA gate passed DNS in8.154s and sibling exact-SHA gates also passed. The first result is a genuine assertion failure, not classified infrastructure; no DNS source or assertion was changed. Determine why expected complete counts are intermittently missing without masking a real scheduler/cardinality defect. Outside loop17 admission envelope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The intermittent count mismatch is attributed with a bounded reproduction or precise evidence identifying its cause
- [x] #2 Any required correction preserves complete delivery, cardinality-cap and checkpoint assertions, with an authentic failing witness and candidate pass
- [x] #3 Race-enabled DNS integration tests pass without skips, count loosening or blind timing retries
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop18 bounded race/count mismatch attribution in isolated worktree, test-only correction if supported with full assertions preserved; red/green and count30 race, security review before landing, composed gate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop18: implementation attempts1; authentic diagnostic red incomplete periodic snapshot14668/20904, exact synchronous final20904. Correction preserves complete-delivery/cardinality/sticky/window/warning assertions. Race count30 passed; guarded security and CodeRabbit clean. Landed594ce5c, exact CI37203796221 ci-success green; composed78f4d46 just check and integrated review green. No image/package changes or new signals, packaging gate not applicable.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed test synchronization by final synchronous OTLP export and independent churn-prefix witnesses, not count relaxation. Verified authentic red/green, race count30, just check, guarded review, exact CI and composed gate.
<!-- SECTION:FINAL_SUMMARY:END -->
