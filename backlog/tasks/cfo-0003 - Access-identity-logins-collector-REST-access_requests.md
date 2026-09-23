---
id: CFO-0003
title: Access identity logins collector (REST access_requests)
status: In Progress
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 20:22'
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
- [ ] #3 Verified against the live account: rows land in Loki on the m7kni stack under service_name=cf2otel
- [x] #4 Metrics: the Groups-derived cloudflare.access.logins by app, allowed, identity provider and login type, plus a REST-derived exact counter cloudflare.access.identity_logins by app, allowed, connection and action; neither carries email, IP, path or ray attributes, and both are listed in docs/signals.md
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: add REST-exact identity counter, then verify a live identity login.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC3/4: cf1AccessLoginsRawGroups lacks connection and action dimensions, so the Groups-backed login metric cannot carry both; raw REST counting would violate the rate contract. Live Access API had zero login rows in the last 3h, so Loki live login proof is absent. Resume with source activity and an approved dimension contract.

Decision 2026-09-23 (Rob): AC3 reworded. Correction to the wave 1 report: REST access_requests is a census, not an adaptive sampled dataset (doc-0003 trap 6 applies to *Adaptive datasets), so counting its rows is exact and does not break the Groups rule. Keep the Groups metric (31d retention, covers service tokens) and add the REST-exact identity-login counter for the connection/action dimensions Groups lacks. The REST counter only covers what the ~1 day REST reach allows; an outage longer than that loses those counts.

Wave 2 REST-exact identity counter is verified by access/logins tests at a3fa01b and listed in docs/signals.md; dimensions are app, allowed, connection and action, with nonidentity rows excluded. Mimir has zero identity-counter series because the post-deploy REST source watcher still sees zero identity rows. AC3 live login proof remains open.
<!-- SECTION:NOTES:END -->
