---
id: CFO-0088
title: Reconcile the extra daily token alert with managed rule ownership
status: To Do
assignee: []
created_date: '2026-10-21 02:01'
labels: []
dependencies: []
priority: medium
type: task
ordinal: 118000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The alert publication workflow applied the generated polling alerts but failed final whole-folder semantic verification because an existing daily input-token alert is outside the repository manifest set. Preserve the existing rule. Decide whether to manage its definition here or give it a separate ownership folder; do not suppress whole-folder verification.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Existing daily token alert intent is preserved and its durable owner is identified
- [ ] #2 Alert sync whole-folder semantic verification passes without deleting useful alerts or ignoring arbitrary extras
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
