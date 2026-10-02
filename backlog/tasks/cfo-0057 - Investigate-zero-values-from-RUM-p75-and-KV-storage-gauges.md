---
id: CFO-0057
title: Investigate zero values from RUM p75 and KV storage gauges
status: In Progress
assignee:
  - '@loop15-root'
created_date: '2026-09-30 21:44'
updated_date: '2026-10-02 14:10'
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
- [ ] #1 Root cause for each is recorded with evidence
- [x] #2 A gauge with no source sample for a window emits no point rather than 0, with a test
- [ ] #3 KV storage values match the source aggregate
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
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 read-only diagnosis at 4b837286829d92db92c47b35a4e208d29c124aab proves RUM replaces previously seen absent/null/-1 quantiles with synthetic zero; the schema marks all negative quantiles N/A. KV code emits no point for empty source windows and preserves genuine numeric zero, so its production drop is not explained by the same defect. Cumulative synchronous SDK gauges can retain previously recorded zeros across empty exports. The supplied nominal 72-hour matrix contains only 63 hourly points spanning 62 hours. AC1/AC2/AC3 remain incomplete. Resume with exact KV source/checkpoint windows, deployed binary/temporality identity, and a bounded real-export omission contract; no global temporality change was silently admitted.

Loop16 RECON-M1 checked source criteria 2 against landed tests at ced4edfbfb66f1b9ee45037bdb425b69820a8ebe. Evidence: codex/evidence-loop16/RECON-return.txt. No new live or browser observation is claimed.
<!-- SECTION:NOTES:END -->
