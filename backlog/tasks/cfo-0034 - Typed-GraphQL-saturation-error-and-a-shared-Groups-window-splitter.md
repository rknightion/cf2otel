---
id: CFO-0034
title: Typed GraphQL saturation error and a shared Groups window splitter
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 16:43'
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
- [x] #1 cfapi exports a SaturationError matched with errors.As and a test fails if any collector string-matches the saturation message
- [x] #2 One shared splitter replaces the per-package split code, with tests for an irreducible bucket and a split on a bucket boundary
- [x] #3 Existing collector tests pass unchanged and just check passes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7 run-end park: implementation attempts 0/4, review-repair 0/3, infrastructure retries 0; grant: typed saturation and shared splitter after L29, with no collector owner overlapping. L34 admitted 11:23:23Z, worktree and frozen brief prepared, but no spawn control completed by 11:53:34Z; root orchestration dispatch stall, not provider outage. Resume with working child dispatch, recheck main and task dependencies, then use the retained l34 brief; no AC is proven.

Loop 8 L34: landed in 8ccd9cf8ee2176f569d34b52010883b9b84dfa44 (lane candidate f45209a, subject refactor(cfapi): typed GraphQL saturation error and shared window splitter). cfapi.SaturationError (Error() byte-identical) plus cfapi.AsSaturation (errors.As first, canonical-message fallback owned by cfapi so the unmodified test fakes still match); collectors call only AsSaturation. Guard test TestCollectorsDoNotStringMatchSaturation fails on the base with 21 violations across all 10 packages, passes at the candidate. Shared splitter internal/collector/splitter.go (SplitWindow, generic Bisect) with irreducible-bucket and bucket-boundary tests; no existing *_test.go modified. just check green at 8ccd9cf; CodeRabbit 15 files 0 findings; independent REV-L34 PASS including scratch equivalence tests of the dropped kv/d1/firewall nudges and email split point (0 mismatches). CI at 8ccd9cf: all 8 workflows success, ci-success success. Loop 8: implementation 1/4 (L34-a1), review-repair 0/3, infrastructure retries 1 (golangci-lint lock), grant: typed saturation and shared splitter; reason Done.
<!-- SECTION:NOTES:END -->
