---
id: CFO-0073
title: Allow loopback literals in the public push scan
status: Done
assignee:
  - '@loop16-root'
created_date: '2026-10-02 13:52'
updated_date: '2026-10-02 14:58'
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
- [x] #1 IPv4 loopback and IPv6 loopback are not scan hits, with a loopback test observed failing on base by assertion.
- [x] #2 An address outside loopback and documentation ranges, embedded IPv6, and a prohibited literal added then removed still fail; tests construct every literal from fragments at runtime.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
PSCAN permits loopback only, proves retained prohibited-address and reachable-history checks using constructed literals, then runs the lane gate and CodeRabbit before exact-SHA review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop16 PSCAN-I1 implementation count 1, review-repair count 0, infrastructure retries 0. REV-PSCAN PASS ffcee3e333ad66292643e6e3fc26a93bf323ada4; exact patch cherry-picked as a325fbe and pushed in 4f14d92189703ab26752f663d5c6b3502e0e2da4. Independent six-case base assertion failure, candidate suite, CodeRabbit zero findings and integrated just check passed. Net and reachable-history scans had no hits; public pushscan five commits zero findings. Landing CI is watched separately, not yet claimed green.
<!-- SECTION:NOTES:END -->
