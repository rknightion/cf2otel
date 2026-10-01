---
id: CFO-0055
title: RUM Web Vitals timings are exported 1000x too large
status: Done
assignee:
  - loop14-root
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 11:29'
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
- [x] #1 A test pins a known microsecond input to its value in seconds for every timing vital
- [x] #2 Live LCP/INP/FCP/TTFB p75 values fall in plausible second ranges after deploy
- [x] #3 The CHANGELOG notes the corrected scale
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 candidate f68e1dd8851103eb9933ffc74a2178934f99f04e regression proves 2500000 microseconds to 2.5 seconds for five timing vitals, CLS unchanged; assertion red on base. just check passed; CodeRabbit all three files covered, minor milliseconds suggestion rejected against frozen live schema. Root corrected shared unit prose; landing 4b837286829d92db92c47b35a4e208d29c124aab passed gen-check/check, scan zero, pushed. Live AC2 and rendered changelog AC3 pending.

Released v0.9.0 CHANGELOG at 42f53d8bf470524a72c898805e99770cdda25071 explicitly says Web Vitals scale reduced by 1000x to seconds. DEP1 was reverted on the coverage startup health error; live AC2 still unchecked.

LIVE55 terminal pass on the healthy v0.10.0 deployment after a completed lagged poll, observed 2026-10-01 09:39 UTC. Backend values: LCP/FCP 0.484 and 20.654 seconds, INP 0.016 seconds, TTFB 0.126 and 20.306 seconds. All required vitals are present in plausible seconds, replacing the prior 1000x scale. Local red witness and exact-candidate gate evidence are retained above. DoD2 is conditional and not applicable: this task changes no Dockerfile, goreleaser or image packaging definition; release image publication succeeded separately.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Corrected GraphQL microseconds to declared seconds, retained CLS behavior, documented the scale correction in the published changelog, and verified the four required live timing vitals after a completed post-deploy window. Full gate and regression witness passed; the live values are no longer scaled as milliseconds under seconds names.
<!-- SECTION:FINAL_SUMMARY:END -->
