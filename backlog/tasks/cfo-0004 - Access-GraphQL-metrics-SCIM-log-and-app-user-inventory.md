---
id: CFO-0004
title: 'Access GraphQL metrics, SCIM log and app/user inventory'
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
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
- [x] #1 Login metrics keep identity and nonidentity (service token/bypass) separable
- [x] #2 SCIM updates emitted as log records with a persisted cursor
- [x] #3 Inventory gauges for apps and users with bounded attributes; app names/domains feed enrichment of the other Access signals
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Access Groups, SCIM cursor and inventory tests passed in exact CI 35875578089. Live m7kni metrics include access apps, users and separated login/request counters.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Access metrics, SCIM and inventory released; tests and live metric families verified.
<!-- SECTION:FINAL_SUMMARY:END -->
