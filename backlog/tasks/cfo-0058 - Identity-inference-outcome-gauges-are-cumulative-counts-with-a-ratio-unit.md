---
id: CFO-0058
title: Identity inference outcome gauges are cumulative counts with a ratio unit
status: In Progress
assignee: []
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 07:34'
labels:
  - access
  - selfobs
dependencies: []
priority: low
type: bug
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf2otel.identity.matched/unmatched/ambiguous are emitted as gauges holding counts since process start, so they reset on restart and carry a misleading _ratio suffix in Prometheus.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The outcomes are counters (or a single counter with an outcome attribute) with a count unit
- [ ] #2 docs/signals.md and the dashboard are updated
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 candidate d094e91f3f910fbe845e3a122a0d6740fd30dc14 real registered identity/SDK tests prove monotonic outcome counters, deltas, unchanged polls, concurrency and replacement-source reset. CodeRabbit finding reproduced/fixed; final full gate green. Root landing used opaque invented fixture identities after the added-line scanner blocked email-shaped synthetic values; expectations unchanged. Documentation updated; dashboard remains DASH2.
<!-- SECTION:NOTES:END -->
