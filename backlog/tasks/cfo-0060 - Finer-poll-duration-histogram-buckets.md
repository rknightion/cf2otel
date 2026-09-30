---
id: CFO-0060
title: Finer poll-duration histogram buckets
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
labels:
  - selfobs
dependencies: []
priority: low
type: enhancement
ordinal: 85000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf2otel.scrape.duration buckets jump from 10 s to 30 s to 60 s, so the collector poll p95 clusters around 29 s and cannot show which collectors are slow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bucket boundaries resolve durations between 1 s and 60 s to within about 25%
- [ ] #2 docs/signals.md lists the new boundaries
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
