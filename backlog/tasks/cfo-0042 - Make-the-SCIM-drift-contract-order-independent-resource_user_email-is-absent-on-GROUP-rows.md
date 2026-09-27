---
id: CFO-0042
title: >-
  Make the SCIM drift contract order-independent: resource_user_email is absent
  on GROUP rows
status: Parked
assignee: []
created_date: '2026-09-27 09:28'
labels:
  - access
  - api-drift
dependencies: []
priority: low
type: bug
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop 10 preparation census with the canary token: 12 SCIM update-log rows on the first page, 6 without resource_user_email (all 5 GROUP rows and 1 USER row). The canary checks only the first row and requires the field unconditionally, so it goes red whenever a GROUP row sorts first. Needs an owner decision on the contract (optional for GROUP, or optional everywhere) before work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The SCIM contract entry accepts the documented GROUP row shape without resource_user_email
- [ ] #2 check_all_rows is enabled for the entry with an order-independent test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
