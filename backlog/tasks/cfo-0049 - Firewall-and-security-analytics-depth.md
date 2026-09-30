---
id: CFO-0049
title: Firewall and security analytics depth
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - parity
  - firewall
dependencies: []
priority: medium
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The firewall metric carries only zone, action and source, so it cannot say which rule fired, on which host, or from where, and rule IDs are opaque without a name. Bot score is a common security view. Parent of the firewall depth subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
