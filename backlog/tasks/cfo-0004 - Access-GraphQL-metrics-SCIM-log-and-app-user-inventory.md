---
id: CFO-0004
title: 'Access GraphQL metrics, SCIM log and app/user inventory'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - access
dependencies: []
priority: medium
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf1AccessLoginsRawGroups (sum.logins, 3h window), accessLoginRequestsAdaptiveGroups (service-token/nonidentity traffic), SCIM updates REST, and inventory gauges from /access/apps and /access/users.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Login metrics keep identity and nonidentity (service token/bypass) separable
- [ ] #2 SCIM updates emitted as log records with a persisted cursor
- [ ] #3 Inventory gauges for apps and users with bounded attributes; app names/domains feed enrichment of the other Access signals
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
