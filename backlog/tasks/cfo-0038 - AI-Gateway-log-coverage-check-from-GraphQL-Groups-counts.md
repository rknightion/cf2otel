---
id: CFO-0038
title: AI Gateway log-coverage check from GraphQL Groups counts
status: To Do
assignee: []
created_date: '2026-09-26 16:05'
labels:
  - ai-gateway
dependencies: []
priority: low
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
REST gateway logs are the primary AI Gateway source. Requests sent with log collection off, or dropped by gateway log limits or retention, never appear in REST. aiGatewayRequestsAdaptiveGroups counts them anyway. Emit a coverage signal comparing the GraphQL Groups request count with the REST-derived request count per gateway over windows held back past ingestion lag (loop 3 measured 139-365 s event-to-Groups; hold back at least 10 minutes). It is a completeness check only, never a second primary rate. Decided by Rob 2026-09-26 when closing CFO-0009.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A metric declared in internal/semconv reports GraphQL Groups minus REST request count per gateway for each lag-safe window, with a test covering equal, missing-logs and GraphQL-behind cases
- [ ] #2 Selections come from settings.availableFields; the collector respects maxDuration and notOlderThan and never double-counts a window on replay
- [ ] #3 Live: after a Camden deploy, one window's GraphQL count, REST count and the emitted value agree on m7kni Mimir
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
