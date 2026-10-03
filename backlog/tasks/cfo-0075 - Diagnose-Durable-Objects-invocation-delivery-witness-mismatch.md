---
id: CFO-0075
title: Diagnose Durable Objects invocation delivery witness mismatch
status: In Progress
assignee:
  - '@loop17-root'
created_date: '2026-10-02 16:01'
updated_date: '2026-10-03 12:08'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 HOUR-0 on the unchanged running release ended after eight reads because durableobjects.invocations had seven attempts but six successes over the observed span. This is a delivery-witness mismatch, not yet an explained API defect; the prepared hour helper is unchanged. The single authorised fresh-hour retry is independent evidence, not a repair.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The attempt-success mismatch is attributed to a collector failure or metric-observation coherence with exact timestamped, sanitised evidence.
- [ ] #2 Any required source correction has an assertion-failing reproduction and candidate pass, without weakening the hour-proof contract.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop17 D75 read-only attribution against loop16 hour receipts and timestamped Loki capture; F75 source repair only if evidence requires it.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 D75 read-only attribution at source 597f2bfe3fedf08c5f9a5594fbe3f6a7e0aa48da: HOUR-0 2026-10-02T14:32:04Z attempt45-to46 with success44 unchanged and errors1-to2 proves failed collector poll, not observation coherence; retry span passes independently. Root timestamped Mimir error-class read confirms class other. Three successful sanitized Loki selections returned no rows, so exact failed source operation remains unknown and no source correction is justified yet. AC1 checked on its stated collector-failure witness; AC2 remains open rather than claiming a source fix. Private evidence D75-return.md and do-error-class.json retained outside tracked paths.
<!-- SECTION:NOTES:END -->
