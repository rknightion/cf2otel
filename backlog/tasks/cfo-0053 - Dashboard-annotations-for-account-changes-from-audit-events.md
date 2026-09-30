---
id: CFO-0053
title: Dashboard annotations for account changes from audit events
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - dashboard
dependencies: []
priority: low
type: enhancement
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Traffic and error changes are easier to explain next to the configuration change that caused them. Audit events already reach Loki.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The shipped dashboard has a toggleable annotation layer of audit events (actor, action, resource) from Loki
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
