---
id: CFO-0051
title: >-
  Operational parity: discovery, filtering, error taxonomy, rate limiting,
  multi-account, pull endpoint
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - parity
  - ops
dependencies: []
priority: medium
type: feature
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operational features other exporters offer that cf2otel lacks. Parent of the ops subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
- [ ] #2 doc-0004 operations section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
