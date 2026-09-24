---
id: CFO-0021
title: 'Zero Trust Gateway DNS, HTTP and network activity'
status: Parked
assignee:
  - '@rob'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 08:23'
labels:
  - 'wave:2'
  - gateway
dependencies: []
priority: low
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf1GatewayDns/Http/Network*RawGroups and rollups, gatewayResolver*, gatewayL4/L7. Only useful once Gateway traffic exists on the account; check first.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Check-then-branch: record whether the account has Gateway traffic before building
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Use the live Gateway DNS Groups branch; negotiate advertised fields, emit bounded DNS metrics, and prove the signal on m7kni. Keep HTTP and network branches pending until source traffic exists.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Prebuild source check: Gateway DNS Groups had traffic in both a recent two-hour window and an 89-day window. Gateway HTTP and network Groups returned zero rows in both windows. Queries used advertised sum fields and succeeded; no account identifiers or row payloads were retained in the tracker.

Loop 3 check-then-branch: recent and 89-day Gateway DNS Groups had nonzero source rows; Gateway HTTP and network Groups had zero rows in both bounded windows. Implemented bounded DNS sum collector at 20e9981 and verified with exact-SHA just check, independent REV and CodeRabbit. Source is pushed but not in Camden v0.3.1, so no live m7kni Gateway DNS signal is claimed. HTTP/network branches remain unbuilt pending natural source traffic. Resume by releasing/deploying the pushed source, waiting for gateway.dns checkpoint, and querying m7kni for cloudflare_gateway_dns_queries_total; only then close the DNS branch. Recheck HTTP/network source activity before those collectors.
<!-- SECTION:NOTES:END -->
