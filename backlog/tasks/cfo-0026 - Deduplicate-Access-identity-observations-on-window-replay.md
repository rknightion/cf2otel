---
id: CFO-0026
title: Deduplicate Access identity observations on window replay
status: Done
assignee: []
created_date: '2026-09-23 17:52'
updated_date: '2026-09-24 07:22'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Window delivery retries after an OTLP export failure, while access.logins observes identities in memory during collection. The current identity index accepts the same login ray again on each retry, consuming candidate capacity and potentially changing match behavior. This was found during Wave 2 review; internal/identity is outside that wave.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Replaying the same Access login row after a failed export leaves one identity candidate and consumes capacity once.
- [x] #2 Distinct login rows remain individually matchable, including rows with the same timestamp.
- [x] #3 A regression test exercises the retry path through the collector and scheduler.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Deduplicate replayed Access rows with bounded non-PII fingerprints while retaining distinct same-time rows; verify scheduler retry behavior and capacity bounds.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Access replay retains one candidate per source row while preserving distinct same-time rows. Prechange retry and collision regressions failed; focused race tests, reviewed integration, exact-SHA just ci, and CI at the v0.3.0 release SHA passed. No new signal names were introduced.
<!-- SECTION:FINAL_SUMMARY:END -->
