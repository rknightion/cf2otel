---
id: CFO-0023
title: >-
  Platform analytics: Workers, D1, KV, R2, Durable Objects, Queues, Workflows,
  Tunnels, Turnstile, Logpush health
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:2'
  - platform
dependencies: []
priority: low
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Enabled account datasets from doc-0003 section 2. Avoid duplicating what the native Workers OTLP export already sends.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A per-dataset decision (build/skip, with reason) is recorded before building
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
