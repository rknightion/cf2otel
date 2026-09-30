---
id: CFO-0059
title: Validate AI Gateway model name extraction
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
labels:
  - aigateway
dependencies: []
priority: low
type: bug
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Some gen_ai.request.model values are empty after the provider prefix or are not model names at all, which splits the per-model cost and token tables.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Model and provider parsing is covered by table tests for prefixed, unprefixed and malformed inputs
- [ ] #2 Unparseable values are mapped to a documented placeholder rather than an empty or arbitrary string
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
