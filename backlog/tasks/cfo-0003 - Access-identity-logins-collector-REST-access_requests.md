---
id: CFO-0003
title: Access identity logins collector (REST access_requests)
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:14'
labels:
  - 'wave:1'
  - access
dependencies: []
priority: high
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Poll /access/logs/access_requests every few minutes with a persisted cursor. REST reach is about a day (doc-0003 trap 3).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Each login is an OTLP log record with user email, user id, IP, country, app uid/name/type, host and path split from app_domain, action, connection, allowed, ray id
- [x] #2 Cursor survives restart and dedupes boundary rows by ray id + created_at; a test proves no duplicates and no gaps
- [ ] #3 Metrics cloudflare.access.logins by app, allowed, connection and action with no email/IP/path attributes
- [ ] #4 Verified against the live account: rows land in Loki on the m7kni stack under service_name=cf2otel
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC3/4: cf1AccessLoginsRawGroups lacks connection and action dimensions, so the Groups-backed login metric cannot carry both; raw REST counting would violate the rate contract. Live Access API had zero login rows in the last 3h, so Loki live login proof is absent. Resume with source activity and an approved dimension contract.
<!-- SECTION:NOTES:END -->
