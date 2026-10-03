---
id: CFO-0078
title: Diagnose intermittent DNS lifetime-cardinality integration assertion
status: To Do
assignee: []
created_date: '2026-10-03 12:21'
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
- [ ] #1 The intermittent count mismatch is attributed with a bounded reproduction or precise evidence identifying its cause
- [ ] #2 Any required correction preserves complete delivery, cardinality-cap and checkpoint assertions, with an authentic failing witness and candidate pass
- [ ] #3 Race-enabled DNS integration tests pass without skips, count loosening or blind timing retries
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
