---
id: CFO-0031
title: Document at-least-once delivery and pin a dedupe key per log family
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-25 14:30'
labels: []
dependencies: []
priority: medium
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Scheduler.commit flushes a window in chunks; if a later chunk fails, the retry re-sends the whole window including chunks already delivered, and an OTLP partial success counts as a payload rejection, so accepted records can be re-sent. No doc states the delivery guarantee. Decision (Rob, 2026-09-25): document at-least-once with a dedupe key per event family rather than change the frozen CFO-0025 commit path.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 docs/troubleshooting.md (or a delivery-semantics section) states the at-least-once guarantee, when re-sends happen, and the dedupe attribute for every log and span family
- [x] #2 A test enumerates every emitted log event family and fails if one lacks its documented dedupe key
- [x] #3 No change to the commit, checkpoint or flush behaviour in internal/collector
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 6: document the at-least-once cases and dedupe keys, and pin every emitted log family to that table with an AST-based test.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
L31 candidate 357bd381eb957076b70b8616938a341a74921da6 landed in 7a7bbdd0986cd6cc60d51823887503c7e655402c. Its AST test failed on the base because the dedupe table was absent, then passed. The diff does not touch internal/collector. just check passed at 7a7bbdd; all nine workflow runs at that SHA concluded success, including ci-success. The integration review corrected checkpoint wording and found zero further issues.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documented at-least-once delivery and dedupe guidance for the emitted log and span families. Verified by the red-then-green AST test, unchanged scheduler files, just check, CodeRabbit, and exact-SHA CI at 7a7bbdd.
<!-- SECTION:FINAL_SUMMARY:END -->
