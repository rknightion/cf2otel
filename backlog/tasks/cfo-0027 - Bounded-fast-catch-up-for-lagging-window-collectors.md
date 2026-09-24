---
id: CFO-0027
title: Bounded fast catch-up for lagging window collectors
status: Done
assignee: []
created_date: '2026-09-23 22:44'
updated_date: '2026-09-24 07:22'
labels:
  - 'wave:3'
dependencies: []
priority: medium
type: enhancement
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
After an OTLP outage, aigateway.logs commits at most one 15-minute source window on each 5-minute scheduler tick. The live wave 2 replay showed that delivery was healthy but checkpoint recovery took much longer than export throughput required. Preserve the 15-minute per-commit safety bound and flush-gated checkpoint behavior while allowing controlled catch-up between normal ticks. The exact concurrency, request budget, and backpressure policy need to be chosen against current code and provider limits when this task is taken up.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A lagging window collector can commit more than one bounded source window per scheduler interval until it approaches live time, without overlapping windows or advancing a checkpoint before export acknowledgement
- [x] #2 Catch-up stops or slows under exporter failure, cancellation, or provider throttling, and retry resumes from the last committed window without losing or duplicating records in a full-exporter-failure test
- [x] #3 A timed test or live measurement shows materially faster recovery from a one-hour lag than one 15-minute window per five-minute tick while retaining the existing 90-second commit deadline
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Commit bounded windows serially inside one tick, stop after failure or cancellation, and prove checkpoint and OTLP delivery behavior with fake-clock and exporter-failure tests.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Lagging collectors can commit multiple 15-minute windows per tick without advancing before export acknowledgement. Prechange catch-up tests failed; fake-clock, cancellation, 429, exporter-failure/retry, fairness and deadline tests passed, followed by independent security review, exact-SHA just ci and CI at v0.3.0.
<!-- SECTION:FINAL_SUMMARY:END -->
