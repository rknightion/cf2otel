---
id: CFO-0094
title: Instrument completed polls and review exporter self-observability
status: In Progress
assignee:
  - '@codex'
created_date: '2026-11-05T22:54:00Z'
labels: []
dependencies: []
priority: high
ordinal: 124000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Aggregate counters cannot reliably attribute source reads, limiter waits, collection and export work to completed polls, especially around restarts. Owner authorized per-poll diagnostics and an OTel-guided review of own-app instrumentation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Completed polls expose accurate request costs and phase timings with success failure and cancellation distinctions
- [ ] #2 Instrumentation reuses provider identity, bounded metric dimensions and useful trace-log correlation without duplicate boundaries or sensitive request content
- [ ] #3 Local receiver tests prove emitted telemetry and documented signal meanings; version and signal-path review records remaining gaps
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
