---
id: CFO-0073
title: Allow loopback literals in the public push scan
status: In Progress
assignee:
  - '@loop16-root'
created_date: '2026-10-02 13:52'
updated_date: '2026-10-02 13:53'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The push scan blocked a reviewed candidate on an allowed loopback listener address. Loop 16 permits loopback only and preserves the other prohibited-literal and reachable-history checks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 IPv4 loopback and IPv6 loopback are not scan hits, with a loopback test observed failing on base by assertion.
- [ ] #2 An address outside loopback and documentation ranges, embedded IPv6, and a prohibited literal added then removed still fail; tests construct every literal from fragments at runtime.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
PSCAN permits loopback only, proves retained prohibited-address and reachable-history checks using constructed literals, then runs the lane gate and CodeRabbit before exact-SHA review.
<!-- SECTION:PLAN:END -->
