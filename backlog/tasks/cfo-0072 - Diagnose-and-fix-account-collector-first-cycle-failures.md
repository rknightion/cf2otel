---
id: CFO-0072
title: Diagnose and fix account collector first-cycle failures
status: Done
assignee:
  - '@loop16-root'
created_date: '2026-10-02 13:52'
updated_date: '2026-10-03 14:14'
labels: []
dependencies: []
priority: high
type: bug
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
After the running release started, 27 account-scoped GraphQL collectors each failed their first cycle with error class other and succeeded on every later cycle. The cause and whether collection was lost or delayed need evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The first-cycle error is explained from saved sanitised error lines and the responsible code path, including whether a data window was lost or delayed.
- [x] #2 A scheduler reproduction with a real HTTP collaborator fails on the base by assertion with the observed error class and passes on the fixed candidate without a first-cycle failure.
- [x] #3 After the next authorised deployment, the sustained-hour evidence supports the fixed first-cycle behaviour, or the live criterion is parked with the exact evidence.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop16 BOOT follows the retained aligned-cursor diagnosis. Use existing WindowLagAt on the twelve affected collector implementations to align ten-minute holdback to closed five-minute boundaries. Through the real scheduler, HTTP collaborator and reopened FileStore, an aligned retained cursor before the next closed bucket is a safe no-query skip; next complete bucket is collected and checkpoint advances. No error-string matching, skipped data or core scheduler weakening. Source owns only mapped Lag implementations plus the scheduler boundary test; live criterion parks behind failed deploy kit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop16 BOOT-D mapped 27 local complete-five-minute-bucket errors and nonadvancing marks, not upstream failure. Preserved predeploy checkpoint snapshot confirms all 27 affected collector cursors at the same aligned timestamp; the second-aligned scheduler upper bound remains within that bucket after restart. These paths return before commit/checkpoint advance and delay rather than intentionally skip data. Proposed existing WindowLagAt alignment awaits public scheduler reproduction and core-chain availability; eventual live recovery not newly proven.

Loop16 source landed ca207018dec85ab25e8a51ab32844e8e4bd26bc8 after fresh high REV-BOOT-R1 PASSbdbe5a04d1898ea4c1dc6d98e815dc9dae915eff. Corrected public fixture independently assertion-red across27 registered scheduler/FileStore/HTTP paths on base, candidate all27 green and full gate repeated; ten-minute lag retained, twelve LagAt additions align closed buckets without validation weakening. BOOT-I1 fixture-pagination gate failed, BOOT-I2 repaired truthful pagination and passed CodeRabbit all13paths/zero findings; two implementations/no infra/no review-repair. Source AC2 checked. AC3 remains unverified behind final failed deploy-kit manifestc7a24d991d3795cee41cb871abe5b3454b0a8edc77b2b15850b169b7ee8f72b9; runtime unchanged0.13.0, no restart or live fixed-version proof. Exact CI pending.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Deployed0.16.0 first10minutes: all27mapped account collectors have cumulative errors0 and no positive error increase at12:38:03Z. HOUR1 then passed65continuousminutes for all49enabled collectors, attempts equal successes and bounded last-success ages. This supports fixed first-cycle behavior without skipping complete buckets or resetting proof after another collector failure; no failed collector in this hour. Exact DEP1 digest/process and boot/hour receipts retained privately.
<!-- SECTION:FINAL_SUMMARY:END -->
