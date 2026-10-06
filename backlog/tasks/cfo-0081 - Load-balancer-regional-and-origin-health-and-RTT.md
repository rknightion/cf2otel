---
id: CFO-0081
title: Load balancer regional and origin health and RTT
status: Parked
assignee: []
created_date: '2026-10-06 06:41'
updated_date: '2026-10-06 06:42'
labels:
  - parity
  - loadbalancers
dependencies: []
ordinal: 110000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Split from CFO-0050.02 (load balancer documented pool traffic). Regional/origin health and RTT cannot be attributed safely while the official pool-health response lacks stable region and origin keys. Waits on Cloudflare documenting stable region/origin keys in the pool health response; no inferred joins or synthetic attribution.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Pool and origin health by region and origin RTT are exported using documented stable region/origin discriminators in the pool health response.
- [ ] #2 Missing or unentitled health is omitted, not fabricated; an account without load balancers produces no errors.
- [ ] #3 The source contract is documented and drift-tested; all new signals and attributes have declarations, signal documentation and dashboard panels.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop25 R1: 0 implementation attempts; tracker-only split. Parked on authoritative Cloudflare stable region/origin keys, not an implementation retry.
<!-- SECTION:NOTES:END -->
