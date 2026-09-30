---
id: CFO-0058
title: Identity inference outcome gauges are cumulative counts with a ratio unit
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
labels:
  - access
  - selfobs
dependencies: []
priority: low
type: bug
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf2otel.identity.matched/unmatched/ambiguous are emitted as gauges holding counts since process start, so they reset on restart and carry a misleading _ratio suffix in Prometheus.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The outcomes are counters (or a single counter with an outcome attribute) with a count unit
- [ ] #2 docs/signals.md and the dashboard are updated
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
