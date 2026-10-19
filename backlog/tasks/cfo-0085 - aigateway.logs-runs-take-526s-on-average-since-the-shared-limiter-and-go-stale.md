---
id: CFO-0085
title: aigateway.logs runs take 526s on average since the shared limiter and go stale
status: To Do
assignee: []
created_date: '2026-10-19 15:14'
labels:
  - bug
dependencies: []
priority: high
ordinal: 115000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured 2026-10-19 on the m7kni stack: aigateway.logs averaged 526 s per run over 24h against 41 s the week before last, with ~28,000 s/day of cf2otel_api_limiter_wait_seconds (about 7.8 h). Only 124 of ~288 expected runs completed, with no scrape errors, so ticks are skipped while a run is still going. Last success has been up to 3,694 s old, tripping cf2otel-collector-stale (900 s) about 16 times in a day. aigateway.coverage is unaffected (288 runs). The timing fits the shared-limiter work in CFO-0079. Do not raise the alert threshold: the alert is correct.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 aigateway.logs completes a run within its polling interval at the p90
- [ ] #2 cf2otel-collector-stale does not fire for aigateway.logs over a 24h window
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
