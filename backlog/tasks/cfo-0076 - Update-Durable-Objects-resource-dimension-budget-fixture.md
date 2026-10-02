---
id: CFO-0076
title: Update Durable Objects resource-dimension budget fixture
status: To Do
assignee: []
created_date: '2026-10-02 17:24'
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
- [ ] #1 The existing optional-budget fixture and selection assertion use the current namespaceId dimension and demonstrate its exclusion under the tight budget.
- [ ] #2 Checkpoint, MAX reduction and no-identifier output assertions remain intact.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
