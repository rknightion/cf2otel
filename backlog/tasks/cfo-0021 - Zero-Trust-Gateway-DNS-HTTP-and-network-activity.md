---
id: CFO-0021
title: 'Zero Trust Gateway DNS, HTTP and network activity'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
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
- [ ] #1 Check-then-branch: record whether the account has Gateway traffic before building
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
