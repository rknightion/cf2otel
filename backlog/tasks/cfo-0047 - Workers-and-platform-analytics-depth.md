---
id: CFO-0047
title: Workers and platform analytics depth
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - parity
  - platform
dependencies: []
priority: high
type: feature
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Platform metrics today are mostly bare account totals: Workers exports only a request count per script, and D1, Durable Objects, Queues, Logpush and R2 omit the errors, latency percentiles, lag and failure signals other exporters expose. Parent of the platform depth subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
- [ ] #2 doc-0004 platform section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
