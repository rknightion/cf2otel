---
id: CFO-0050
title: >-
  Origin and edge health: certificates, load balancers, health checks, status
  page
status: Done
assignee: []
created_date: '2026-09-30 21:17'
updated_date: '2026-10-06 06:52'
labels:
  - parity
  - health
dependencies: []
priority: medium
type: feature
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Certificate expiry, load balancer pool/origin health, health check results and Cloudflare incident status are cheap, high-value alerting signals that other exporters provide and cf2otel does not. Parent of the health subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All subtasks are Done
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop25 tracker-only reconciliation: verify all four child statuses and all four health checklist rows, preserve top-level source-prerequisite park, and run final tracker-tree just check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop25 R1: 0 implementation attempts; tracker reconciliation only. Parent remains open while CFO-0050.04 (Cloudflare platform status from the public status page) awaits definitive root green exact CI and composed G-SP, and the health checklist is not fully checked. CFO-0081 (Load balancer regional and origin health and RTT) is a separate top-level source-prerequisite park. No parent closure before every child is Done and the whole health section is checked.

Loop25 final reconciliation: all four children verified Done through CLI readbacks and all four doc-0004 (parity checklist) health rows checked. Root-verified statuspage exact CI 37424290375 and composed G-SP exit0 at 6e7e046e90d6cfb7f24032bb0ffd6b4203d80d9c satisfy the last child prerequisite. Parent closure covers delivered admitted scope only; regional/origin health and RTT remains top-level Parked CFO-0081 (Load balancer regional and origin health and RTT). R1 consumes 0 implementation attempts. Image DoD not applicable to tracker-only reconciliation; declarations/docs criterion inherited from checked child evidence.

Loop25 R1 review repair round 1: removed private machine evidence paths from newly added public tracker text; safe evidence hashes, exact gate/CI identities and scope qualifications retained. No implementation attempts charged; rejected candidate remains local-only and corrected tracker diff is integrated onto the original safe base without rejected ancestry.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Closed after CLI verification that certificate, documented load-balancer traffic, health-check analytics and public statuspage children are all Done and the whole parity health section is checked. No blocked regional/origin scope or new live proof claimed; that scope remains CFO-0081 (Load balancer regional and origin health and RTT), top-level Parked pending stable documented keys.
<!-- SECTION:FINAL_SUMMARY:END -->
