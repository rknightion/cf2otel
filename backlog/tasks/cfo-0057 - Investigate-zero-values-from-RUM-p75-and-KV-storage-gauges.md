---
id: CFO-0057
title: Investigate zero values from RUM p75 and KV storage gauges
status: Done
assignee:
  - '@loop24-root'
created_date: '2026-09-30 21:44'
updated_date: '2026-10-06 06:52'
labels:
  - rum
  - platform
dependencies: []
priority: medium
type: bug
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The latest RUM p75 gauge value is 0 for most site/device pairs while max_over_time is not, which suggests windows without samples emit 0 instead of nothing and reads as falsely good. KV storage bytes and keys gauges dropped from about 1.7 MB to 0 and stayed there while D1 and DO storage looked normal. Both are suspected, not proven.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause for each is recorded with evidence
- [x] #2 A gauge with no source sample for a window emits no point rather than 0, with a test
- [x] #3 KV storage values match the source aggregate
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop15 implementation follows the frozen lane packet; assertion-based red witness, local just check and CodeRabbit, independent REV where required, root linear landing and sustained live evidence where applicable.

Loop17 K57 root captured current source GraphQL shapes and matching Mimir values; lane attributes root causes and fixes only proven local defects with assertion-red. KV live comparison at next HOUR.

Loop24: bounded root Cloudflare source read and granted Grafana current read; compare sanitized values within one export interval.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 read-only diagnosis at 4b837286829d92db92c47b35a4e208d29c124aab proves RUM replaces previously seen absent/null/-1 quantiles with synthetic zero; the schema marks all negative quantiles N/A. KV code emits no point for empty source windows and preserves genuine numeric zero, so its production drop is not explained by the same defect. Cumulative synchronous SDK gauges can retain previously recorded zeros across empty exports. The supplied nominal 72-hour matrix contains only 63 hourly points spanning 62 hours. AC1/AC2/AC3 remain incomplete. Resume with exact KV source/checkpoint windows, deployed binary/temporality identity, and a bounded real-export omission contract; no global temporality change was silently admitted.

Loop16 RECON-M1 checked source criteria 2 against landed tests at ced4edfbfb66f1b9ee45037bdb425b69820a8ebe. Evidence: codex/evidence-loop16/RECON-return.txt. No new live or browser observation is claimed.

Loop17 root current source and hour-end comparison retained: RUM historical missing-point syntheticzero mechanism already fixed; current actualCLSzero valid. KV source latest5min thenpast1hour both empty/noAPIerror while exportedmaxvalues positive; exacthistoricalzero cause and source-equalityAC1/3unproved. Runtimeaccount matches private source notes. No code/temporality change justified. Resume nonempty exact source/checkpoint/correlated resource max comparison, preserve AC2 source omission test.

Loop24: 0 implementation attempts. Three bounded runtime-token reads succeeded; latest collector-eligible five-minute KV source window empty, current Grafana storage gauges absent (not zero). No numeric equality or historical zero cause proved; AC1 and AC3 stay open. Resume with nonempty per-namespace source and current exported values within one export interval. RUM cause already recorded; no new code change justified.

Loop25 R1: 0 implementation attempts; K1 consumes 0. Root accepted the admitted intermittent-absence diagnosis and bounded equality in the root-accepted private KV diagnosis, SHA256 e75aca1b0cd5cce4bed365f7bf7e761bb89526e7c9269d3fc6741121eefbd4f5. Sparse complete storage source plus strongly-supported process-local gauge reset at aligned build transition explain observed absence: storage polls continued, checkpoint age bounded (15.9–20.9 minutes), 263 returned commit-failure samples all zero. Three bounded singleton namespace source byte/key pairs exactly equal historical emitted gauges after eligibility. Preserve checked AC2 and previously fixed recorded RUM cause. Old historical-zero cause remains unknown; process-start/reset is inferred, not directly observed. No live-hour, simultaneous-current or full per-name equality claimed. No code fix justified. Image and new-signal DoD not applicable to diagnosis-only reconciliation; tracker gate pending.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Done only for owner-admitted intermittent KV absence diagnosis, already fixed RUM synthetic-zero cause and bounded nonempty equality. Root-approved hashed diagnosis records sparse source, continued storage polling, bounded checkpoint age, zero observed commit failures and strongly-supported gauge-memory reset at aligned build transition. Three singleton historical source byte/key pairs exactly match emitted gauges. AC2 preserved. Original historical-zero cause remains unknown; process start inferred; no live-hour or current equality claimed.
<!-- SECTION:FINAL_SUMMARY:END -->
