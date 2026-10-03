---
id: CFO-0076
title: Update Durable Objects resource-dimension budget fixture
status: Done
assignee:
  - '@loop17-root'
created_date: '2026-10-02 17:24'
updated_date: '2026-10-03 12:53'
labels: []
dependencies: []
priority: low
type: chore
ordinal: 100000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 source review found an optional-field-budget fixture still advertises namespaceName while production correctly selects namespaceId. Existing checkpoint and MAX assertions pass, but this fixture no longer demonstrates exclusion of the new dimension under a tight field budget. This is a test coverage gap, not an observed production defect.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The existing optional-budget fixture and selection assertion use the current namespaceId dimension and demonstrate its exclusion under the tight budget.
- [x] #2 Checkpoint, MAX reduction and no-identifier output assertions remain intact.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop17 frozen packet: one local implementation attempt with assertion-red witness, just check and CodeRabbit to terminal result; fresh exact-SHA independent REV before root linear landing. No lane remote writes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 fresh REV-FIX-PLAT found no code defect and supports namespaceId tight-budget fixture/checkpoint/MAX/no-identifier evidence, but combined candidate blocked by separate CFO-0077 missing red witness. Root review-repair RR1 of3 admits same-branch narrow withdrawal of both D1 hardening edits, retaining only Durable Objects fixture patch; no gate or criterion relaxed, fresh REV on new full SHA required.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Durable Objects tight-field-budget fixture advertises current namespaceId and proves exclusion under the two-field budget, preserving checkpoint/MAX14/no-identifier assertions. Fixture-only base tests green explicitly not called product red. Fresh REV-FIX-PLAT-RR1 PASSbfefab20fba6ed227d0e566d60ae84d213fbb304; exact patch/history landed87b845c/be8c345, D1 hardening restored byte-identicalbase and CFO0077 remainsparked. Integrated gate/scans green and pushed061690dcd59effbbdfe0cc116025361ba8b39488, landing CI pending. No image/packaging or new signal obligation applies.
<!-- SECTION:FINAL_SUMMARY:END -->
