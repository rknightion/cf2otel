---
id: CFO-0002
title: 'Cloudflare API client: read-only REST + GraphQL with entitlement-aware queries'
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 20:15'
labels:
  - 'wave:1'
  - cfapi
dependencies: []
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/cfapi. The only package that talks to Cloudflare. See doc-0003 for the live-verified contract and traps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Any method other than GET, or POST to /graphql, is refused before network I/O, and a test proves it
- [x] #2 Retries 429/502/503/504 honouring Retry-After, else bounded exponential backoff; caps response size; sets timeouts; wraps the transport for cf2otel.* HTTP client metrics and spans
- [x] #3 Entitlement errors are matched case-insensitively and cause a field-set renegotiation, not a crash loop
- [x] #4 Zone and gateway discovery via GET /zones and /ai-gateway/gateways when no list is configured
- [x] #5 Fixtures under internal/cfapi/testdata are sanitized per the wave operating model
- [x] #6 GraphQL selections are built from settings.availableFields intersected with the wanted fields and split to respect maxNumberOfFields; windows are split to respect maxDuration; a window reaching before notOlderThan returns a typed retention-gap error carrying the retention floor, and the scheduler advances the checkpoint to that floor and emits a cf2otel.window.gap log event and a cf2otel.window.gap counter (seconds skipped) instead of failing every tick
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: use typed retention floors and advance past an explicit disclosed gap.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC3: client fails closed on a notOlderThan retention gap instead of clamping the requested window. This preserves gap visibility but does not satisfy the literal clamp criterion; decide contract wording or implement an explicit partial window with disclosure.

Decision 2026-09-23 (Rob): AC3 reworded. Wave 1 failed closed on a retention gap, which never self-heals: the scheduler never advances, so a window older than notOlderThan (31d on the HTTP and Access Groups datasets, or a misconfigured initial_lookback) stalls that collector permanently. Silent clamping was rejected because it hides loss. Contract: skip forward to the retention floor with explicit gap disclosure. Implemented in wave 2 alongside CFO-0025.

Correction: the reworded retention criterion is AC6 (the old AC3 was removed and the new text appended). Wave 2 prep also found the skip-forward must use a margin past the floor, because the cutoff is computed as time.Now()-notOlderThan when the query runs and so moves forward every tick.

Wave 2 AC6 verified at released source a3fa01b2a16c2f8fd56381848437406f44330774: TestGraphQLNegotiatesAndSplits, TestGraphQLRetentionGapCarriesFloor, TestGraphQLRetentionGapDoesNotQuery, TestRetentionGapSkipsPastMovingFloor and TestRetentionGapUsesLatestReportedFloor passed under just check; just ci passed before release. The explicit cf2otel.window.gap signal is declared in internal/semconv and docs/signals.md. Live firewall probing also found that settings.availableFields can advertise fields the query schema rejects; doc-0003 records that exception.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Wave 2 completed typed retention-gap skip-forward with an explicit gap log and counter, plus entitlement/window splitting. Verified by named tests, just check, just ci and the v0.2.2 release source.
<!-- SECTION:FINAL_SUMMARY:END -->
