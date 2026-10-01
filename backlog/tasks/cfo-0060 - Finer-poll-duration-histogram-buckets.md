---
id: CFO-0060
title: Finer poll-duration histogram buckets
status: Done
assignee: []
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 07:32'
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
- [x] #1 Bucket boundaries resolve durations between 1 s and 60 s to within about 25%
- [x] #2 docs/signals.md lists the new boundaries
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Frozen finer duration boundaries and documented lists verified through production provider/local HTTP OTLP export with 5/15/45-second samples, count 3 and sum 65 on both scrape and API histograms. Base failed by assertion, candidate 2384dffb17625fabc46916a53477c37ffe2a0d34 passed full gate and three-file CodeRabbit review. A8 changed only the existing API sample boundary expectation from removed 0.05 to 0.25, preserving count assertion. Root landing 1a98fab8bf864a11fa0d065e882fa05bad6d35cd passed gate and scan; deployment not claimed. No packaging change.
<!-- SECTION:FINAL_SUMMARY:END -->
