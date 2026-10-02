---
id: CFO-0074
title: Validate minimum windows for bucket-aligned collectors
status: To Do
assignee: []
created_date: '2026-10-02 15:55'
labels: []
dependencies: []
priority: low
type: bug
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 independent source review found that the opt-in Workers AI collector requires a complete five-minute source bucket while configuration validation accepts any positive max_window. A shorter configured window can repeatedly fail without advancing. Defaults are unaffected; runtime reproduction is still required.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A public configuration and scheduler reproduction demonstrates the sub-bucket window failure by assertion before correction.
- [ ] #2 Incompatible enabled collector window settings are rejected early or have a documented supported treatment, without weakening existing source-window and checkpoint guarantees.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
