---
id: CFO-0055
title: RUM Web Vitals timings are exported 1000x too large
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
labels:
  - rum
dependencies: []
priority: high
type: bug
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/collectors/rum divides the GraphQL microsecond quantiles by 1000 (giving milliseconds) but the metrics are declared in seconds, so cloudflare_rum_*_p75_seconds reads e.g. 5560 for a 5.56 s LCP. The dashboard follows the declared unit and shows hours.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A test pins a known microsecond input to its value in seconds for every timing vital
- [ ] #2 Live LCP/INP/FCP/TTFB p75 values fall in plausible second ranges after deploy
- [ ] #3 The CHANGELOG notes the corrected scale
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
