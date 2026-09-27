---
id: CFO-0042
title: >-
  Make the SCIM drift contract order-independent: resource_user_email is absent
  on GROUP rows
status: Parked
assignee: []
created_date: '2026-09-27 09:28'
updated_date: '2026-09-27 12:22'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 11: implementation L42-a1 used (1/4); review-repair 0/3; infrastructure retries 0. Grant: any-row SCIM rule. Candidate commit d641dadd80d5befe35266f52910625873d4222ae passed exact-SHA just check and CodeRabbit complete (3 files, no unreviewed); red-then-green order-independent tests. CodeRabbit suggested suppressing a difference on pages without USER rows, but that contradicts the frozen owner rule requiring the field in at least one nonempty-page row. Not landed: mandatory independent REV-C was not dispatched after a root wait/dispatch failure. Resume with REV-C PASS on this exact SHA, pre-land live check zero differences, landing scan/CI, then R0 success; retain l11-lc.
<!-- SECTION:NOTES:END -->
