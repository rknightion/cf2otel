---
id: CFO-0046
title: HTTP zone analytics parity with Prometheus-style Cloudflare exporters
status: To Do
assignee: []
created_date: '2026-09-30 21:15'
labels:
  - parity
  - http
dependencies: []
priority: high
type: feature
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Common Cloudflare exporters treat bandwidth, geography, exact status, origin errors and latency percentiles as core zone metrics. cf2otel HTTP metrics carry only zone, host, status class and cache status, with an origin-duration average and no bytes at all. Cardinality policy (Rob, 2026-09-30): low-cardinality dimensions on by default, high-cardinality ones opt-in, all under the existing series caps. Parent of the HTTP parity subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
- [ ] #2 doc-0004 HTTP section is fully checked off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
