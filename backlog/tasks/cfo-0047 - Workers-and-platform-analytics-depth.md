---
id: CFO-0047
title: Workers and platform analytics depth
status: Done
assignee: []
created_date: '2026-09-30 21:17'
updated_date: '2026-10-03 15:10'
labels:
  - parity
  - platform
dependencies: []
priority: high
type: feature
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Platform metrics today are mostly bare account totals: Workers exports only a request count per script, and D1, Durable Objects, Queues, Logpush and R2 omit the errors, latency percentiles, lag and failure signals other exporters expose. Parent of the platform depth subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All subtasks are Done
- [x] #2 doc-0004 platform section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Loop17 fresh child status read confirms every subtask Done and full corresponding doc-0004 section checked against recorded exact-source acceptance. Latest containing source gate and landing CI green; parent closure is source parity, not a new universal live/browser claim.
<!-- SECTION:FINAL_SUMMARY:END -->
