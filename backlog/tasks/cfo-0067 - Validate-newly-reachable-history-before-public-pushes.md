---
id: CFO-0067
title: Validate newly reachable history before public pushes
status: In Progress
assignee:
  - '@loop15-root'
created_date: '2026-10-01 11:17'
updated_date: '2026-10-01 18:47'
labels:
  - security
  - tooling
dependencies: []
priority: high
type: bug
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 independent drift review proved a net-tree added-line scan can miss prohibited synthetic email fixtures added in one new commit and removed by a later commit before the same push. The tip was sanitized but both original commits remained published ancestors. No real-person identifier or credential was found in that Git ancestry finding. History rewrite is not authorized. A private loop14 reachability-scan witness correctly rejects the affected historical range, but the durable pre-push validation surface still needs to carry this protection for future work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Pre-push privacy validation checks additions in every newly reachable commit as well as the final tree diff
- [ ] #2 An assertion-based fixture adding then removing a prohibited literal is rejected even though its net tree diff is clean
- [ ] #3 Validation retains documented fixture exceptions without silently broadening identifier allowlists
- [ ] #4 Historical publication exceptions are reported accurately without rewriting history
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop15 implementation follows the frozen lane packet; assertion-based red witness, local just check and CodeRabbit, independent REV where required, root linear landing and sustained live evidence where applicable.
<!-- SECTION:PLAN:END -->
