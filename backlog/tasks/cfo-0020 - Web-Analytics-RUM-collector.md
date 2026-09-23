---
id: CFO-0020
title: Web Analytics / RUM collector
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:2'
  - rum
dependencies: []
priority: low
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
rumPageloadEventsAdaptiveGroups and rumWebVitalsEventsAdaptive(Groups) with ingestion-lag hold-back (doc-0004).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Parity with doc-0004 RUM section
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
