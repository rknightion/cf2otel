---
id: CFO-0063
title: Prove current certificate status and expiry across snapshot changes
status: In Progress
assignee:
  - '@loop15-root'
created_date: '2026-10-01 07:32'
updated_date: '2026-10-01 18:47'
labels:
  - certs
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 87000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 independent certificate review found that status is part of a synchronous gauge attribute set. The cumulative SDK can keep exporting earlier status series and their old seconds-to-expiry values after a later snapshot changes the pack. Current-status and expiry alert interpretation cannot be claimed solely from an emitted series or a green generator; the owned collector tests do not yet prove retirement across real SDK exports.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A real SDK/export-boundary reproduction distinguishes current certificate pack state from retained prior status series
- [ ] #2 Successful changed and empty snapshots do not leave stale positive expiry or status evidence usable as current state
- [ ] #3 Certificate dashboard and alert queries are verified against the corrected snapshot semantics
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
