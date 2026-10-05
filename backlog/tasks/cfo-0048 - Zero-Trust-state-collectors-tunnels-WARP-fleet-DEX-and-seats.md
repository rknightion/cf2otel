---
id: CFO-0048
title: 'Zero Trust state collectors: tunnels, WARP fleet, DEX and seats'
status: Done
assignee:
  - '@loop24-root'
created_date: '2026-09-30 21:17'
updated_date: '2026-10-05 22:24'
labels:
  - parity
  - zerotrust
dependencies: []
priority: high
type: feature
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Zero Trust exporters expose live state rather than events: tunnel health, connected WARP devices, Digital Experience test results and seat usage. cf2otel covers Access logins and app/user counts but no state surface. These are REST snapshot collectors (the inventory.access RegisterSnapshot shape) and need token permissions cf2otel does not request today. Parent of the Zero Trust state subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All subtasks are Done
- [x] #2 doc-0004 Zero Trust section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop24 R1: all four child tasks are Done and doc-0004 Zero Trust section fully checked. Finalize parent after tracker gate/review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop24 R1: 0 implementation attempts. All four children are Done and doc-0004 Zero Trust section fully checked, including raw-status WARP decision. Child source landings reviewed/gated and required CI green; root tracker precommit just check passed on7fdf297f899a6a38d0b8857f8faaf60b7538645e with backlog-only changes. No package/image changes so packaging-specific DoD is not applicable. Parent marker published only after root final review and bounded path commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
All Zero Trust state child contracts accepted and their parity checklist complete. WARP counts are raw-status aggregates with fixture-proved full pagination, not inferred connected-device classification or new populated live proof.
<!-- SECTION:FINAL_SUMMARY:END -->
