---
id: CFO-0052
title: 'Decide and build Workers AI, Hyperdrive and Vectorize analytics'
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - platform
dependencies: []
priority: low
type: spike
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Newer developer-platform products have GraphQL datasets no comparable exporter covers yet. Record a per-dataset build/skip decision as CFO-0023 did, then build the chosen ones.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A per-dataset build/skip decision with reason is recorded
- [ ] #2 Chosen datasets are built
- [ ] #3 A new dataset or endpoint has a live-verified entry in doc-0003 and an API drift canary probe
- [ ] #4 Every new signal and attribute is declared in internal/semconv, listed in docs/signals.md, and has a panel in grafana/build_dashboard.py
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
