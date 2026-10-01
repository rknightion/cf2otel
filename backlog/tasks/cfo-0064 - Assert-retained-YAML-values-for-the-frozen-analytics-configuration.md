---
id: CFO-0064
title: Assert retained YAML values for the frozen analytics configuration
status: To Do
assignee: []
created_date: '2026-10-01 07:32'
labels:
  - config
dependencies: []
priority: low
type: task
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 seam review passed the configuration contract but identified a regression-coverage gap: valid YAML cases currently assert successful validation without checking the retained zero cardinality, all request-source policy and explicit empty breakdown list. Environment cases assert values directly; a YAML-specific ignored-value regression could pass existing tests.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Existing YAML integration cases assert retained zero cardinality, all request source and empty breakdown values
- [ ] #2 A deliberate ignored-value mutation makes the relevant assertions fail without relying on a compile error
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
