---
id: CFO-0048
title: 'Zero Trust state collectors: tunnels, WARP fleet, DEX and seats'
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - parity
  - zerotrust
dependencies: []
priority: high
type: feature
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Zero Trust exporters expose live state rather than events: tunnel health, connected WARP devices, Digital Experience test results and seat usage. cf2otel covers Access logins and app/user counts but no state surface. These are REST snapshot collectors (the inventory.access RegisterSnapshot shape) and need token permissions cf2otel does not request today. Parent of the Zero Trust state subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
- [ ] #2 doc-0004 Zero Trust section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
