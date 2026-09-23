---
id: CFO-0009
title: AI Gateway GraphQL metrics and REST/GraphQL reconciliation
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 20:07'
labels:
  - 'wave:1'
  - aigw
dependencies: []
priority: medium
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
aiGatewayRequestsAdaptiveGroups, aiGatewayErrorsAdaptiveGroups, aiGatewayCacheAdaptiveGroups. doc-0003 trap 7: the Groups dataset returned zero rows while REST had fresh logs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause of trap 7 is established and recorded in doc-0003 (or the dataset is dropped with the evidence)
- [ ] #2 If used, GraphQL-derived metrics agree with REST-derived counts within the sampling tolerance over a measured window
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
W8 read-only reconciliation on 2026-09-23: identical 3h GraphQL Groups window initially returned 0 with 38 REST requests (latest 13.9m old), then later returned 38; other 1h and 3h windows matched 2/2 and 39/39. Unknown upper lag. Dropped GraphQL collector from registration; REST supplies bounded metrics. Corrected doc-0003 trap 7 via Backlog CLI.

Parked: GraphQL AI Gateway Groups changed from zero to matching REST counts for an unchanged 3h window after unbounded ingestion lag. REST metrics are enabled and GraphQL collector disabled. Resume only with measured upper lag and a reconciliation tolerance; AC2 is conditional and not exercised.

Wave 2 R1 measurement (2026-09-23, 13 samples at 15-minute cadence, each query a matching rolling 3h window): REST unique log IDs and aiGatewayRequestsAdaptiveGroups count agreed at every sample (13/13). The table records age of the newest REST row as a source freshness observation, not a measured GraphQL ingestion latency. No count mismatch appeared, so this experiment does not establish a safe upper ingestion lag or satisfy AC2; GraphQL metrics remain disabled.

| UTC sample | REST IDs | GraphQL count | Newest REST row age (s) |
| --- | ---: | ---: | ---: |
| 2026-09-23T17:05:33 | 196 | 196 | 709 |
| 2026-09-23T17:21:05 | 197 | 197 | 335 |
| 2026-09-23T17:36:08 | 187 | 187 | 1238 |
| 2026-09-23T17:51:10 | 183 | 183 | 2140 |
| 2026-09-23T18:06:13 | 183 | 183 | 3043 |
| 2026-09-23T18:21:15 | 196 | 196 | 323 |
| 2026-09-23T18:36:18 | 194 | 194 | 1226 |
| 2026-09-23T18:51:20 | 194 | 194 | 2128 |
| 2026-09-23T19:06:22 | 194 | 194 | 313 |
| 2026-09-23T19:21:25 | 145 | 145 | 1216 |
| 2026-09-23T19:36:27 | 88 | 88 | 2118 |
| 2026-09-23T19:51:28 | 27 | 27 | 3019 |
| 2026-09-23T20:06:30 | 22 | 22 | 375 |

Observed age range 313-3043 seconds; maximum 3043 seconds reflected idle source traffic, not GraphQL delay. Resume AC2 with event-correlated arrival observations or a bounded lag measured at finer cadence, then select reconciliation tolerance before enabling the GraphQL collector.
<!-- SECTION:NOTES:END -->
