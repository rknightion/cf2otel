---
id: CFO-0072
title: Diagnose and fix account collector first-cycle failures
status: To Do
assignee: []
created_date: '2026-10-02 13:52'
updated_date: '2026-10-02 14:19'
labels: []
dependencies: []
priority: high
type: bug
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
After the running release started, 27 account-scoped GraphQL collectors each failed their first cycle with error class other and succeeded on every later cycle. The cause and whether collection was lost or delayed need evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The first-cycle error is explained from saved sanitised error lines and the responsible code path, including whether a data window was lost or delayed.
- [ ] #2 A scheduler reproduction with a real HTTP collaborator fails on the base by assertion with the observed error class and passes on the fixed candidate without a first-cycle failure.
- [ ] #3 After the next authorised deployment, the sustained-hour evidence supports the fixed first-cycle behaviour, or the live criterion is parked with the exact evidence.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop16 BOOT-D mapped 27 local complete-five-minute-bucket errors and nonadvancing marks, not upstream failure. Preserved predeploy checkpoint snapshot confirms all 27 affected collector cursors at the same aligned timestamp; the second-aligned scheduler upper bound remains within that bucket after restart. These paths return before commit/checkpoint advance and delay rather than intentionally skip data. Proposed existing WindowLagAt alignment awaits public scheduler reproduction and core-chain availability; eventual live recovery not newly proven.
<!-- SECTION:NOTES:END -->
