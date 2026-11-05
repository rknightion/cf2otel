---
id: CFO-0093
title: Overlap AI log detail reads across seconds and reuse bounded fetched details
status: In Progress
assignee:
  - '@codex'
created_date: '2026-11-05T22:54:00Z'
labels: []
dependencies: []
priority: high
ordinal: 123000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Budgeted AI log collection restarts prefetch for every source second, serializing sparse traffic. Overflow seconds are fetched again on the next commit. Owner authorized cross-second overlap and bounded reuse while retaining whole-second checkpoint safety.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Sparse records across seconds overlap detail reads with bounded concurrency and retain ordered complete-second delivery
- [ ] #2 Fetched details are reused across budget cuts and retries with explicit memory and lifetime bounds and no gateway or window contamination
- [ ] #3 Cancellation joins workers; failures never advance checkpoints; real-boundary regressions demonstrate reduced requests without lost records
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
