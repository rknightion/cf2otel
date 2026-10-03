---
id: CFO-0077
title: Harden platform HTTP fixture synchronization
status: Parked
assignee:
  - '@loop17-root'
created_date: '2026-10-02 17:38'
updated_date: '2026-10-03 12:17'
labels: []
dependencies: []
priority: low
type: chore
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 CodeRabbit review of the platform canary repair raised test-only synchronization concerns for phase in the D1 cap fixture and restCalls in the D1 name fixture. The complete race gate passed, so no production race or failing reproduction is claimed; the findings remain useful targeted test-harness hardening work outside this delivery slice.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The two named HTTP fixtures use explicit safe synchronization for state shared between test control and server handlers.
- [ ] #2 Existing public boundary assertions for counts, names, caps and read-only calls remain unchanged and race-enabled fixture tests pass.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop17 frozen packet: one local implementation attempt with assertion-red witness, just check and CodeRabbit to terminal result; fresh exact-SHA independent REV before root linear landing. No lane remote writes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 FIX-PLAT candidate sound atomic fixture hardening and full gate passed, but required frozen packet base race red was not observed by worker or fresh independent reviewer (150 repetitions per named test plus full race package all passed). No false race-elimination pass or gate waiver. Preserve combined candidate d77f62315412dc73342a652393164ae7382e7524; withdraw D1 changes from forthcoming CFO-0076-only candidate. Resume only with authentic base race witness or owner-approved changed prerequisite.
<!-- SECTION:NOTES:END -->
