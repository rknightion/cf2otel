---
id: CFO-0058
title: Identity inference outcome gauges are cumulative counts with a ratio unit
status: Done
assignee: []
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 15:43'
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
- [x] #2 docs/signals.md and the dashboard are updated
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

Final DASH2 native readback confirms panel 2331 uses cf2otel_identity_outcomes_total grouped by outcome, and panel 2306 derives the match ratio from outcome counter rates; docs/signals.md declares counter/count semantics. Original registered SDK tests prove delta-only increments, unchanged polls, concurrency and replacement-source reset. Public ancestry retains prohibited synthetic email fixtures from the original candidate despite sanitized tip; this literal-policy exception is acknowledged separately, not a real-person leak claim. No packaging change; conditional DoD2 does not apply.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Replaced cumulative ratio-named outcome gauges with monotonic count-unit counters and updated documentation and shipped counter-based dashboard queries. Registered real SDK delta/reset behavior passed.
<!-- SECTION:FINAL_SUMMARY:END -->
