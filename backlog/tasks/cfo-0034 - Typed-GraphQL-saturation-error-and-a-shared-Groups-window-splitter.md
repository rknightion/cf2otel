---
id: CFO-0034
title: Typed GraphQL saturation error and a shared Groups window splitter
status: Parked
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 13:30'
labels: []
dependencies:
  - CFO-0024
priority: low
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/cfapi/graphql.go reports saturation as a formatted string and nine collector packages detect it with strings.Contains (two lowercase it first, so copies already differ); each also carries its own five-minute bucket splitter. cfapi already has typed FieldLimitError and RetentionGapError. Where the shared splitter lives is a seam decision: internal/collector owns windows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 cfapi exports a SaturationError matched with errors.As and a test fails if any collector string-matches the saturation message
- [ ] #2 One shared splitter replaces the per-package split code, with tests for an irreducible bucket and a split on a bucket boundary
- [ ] #3 Existing collector tests pass unchanged and just check passes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7 run-end park: implementation attempts 0/4, review-repair 0/3, infrastructure retries 0; grant: typed saturation and shared splitter after L29, with no collector owner overlapping. L34 admitted 11:23:23Z, worktree and frozen brief prepared, but no spawn control completed by 11:53:34Z; root orchestration dispatch stall, not provider outage. Resume with working child dispatch, recheck main and task dependencies, then use the retained l34 brief; no AC is proven.
<!-- SECTION:NOTES:END -->
