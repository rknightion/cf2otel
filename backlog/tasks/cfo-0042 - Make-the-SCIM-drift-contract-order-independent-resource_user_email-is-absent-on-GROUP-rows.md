---
id: CFO-0042
title: >-
  Make the SCIM drift contract order-independent: resource_user_email is absent
  on GROUP rows
status: Done
assignee: []
created_date: '2026-09-27 09:28'
updated_date: '2026-09-27 17:03'
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
- [x] #1 The SCIM contract entry accepts the documented GROUP row shape without resource_user_email
- [x] #2 check_all_rows is enabled for the entry with an order-independent test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 11: implementation L42-a1 used (1/4); review-repair 0/3; infrastructure retries 0. Grant: any-row SCIM rule. Candidate commit d641dadd80d5befe35266f52910625873d4222ae passed exact-SHA just check and CodeRabbit complete (3 files, no unreviewed); red-then-green order-independent tests. CodeRabbit suggested suppressing a difference on pages without USER rows, but that contradicts the frozen owner rule requiring the field in at least one nonempty-page row. Not landed: mandatory independent REV-C was not dispatched after a root wait/dispatch failure. Resume with REV-C PASS on this exact SHA, pre-land live check zero differences, landing scan/CI, then R0 success; retain l11-lc.

Loop 12: implementation 1/4 (L42-a1); review-repair 1/3 (L42-rr1); infrastructure retries 0. Grant: exact-SHA review, narrow null-rule repair, main landing and read-only canary. REV-C2 PASS on 661d3a7014dd166adece9b4f386d90eef3b87459; pre-land live contract matched; merge 4c951399680d8dc50725da7df94b0ec3ceb42dc4 passed just check and all eight workflows, including CI ci-success and actionlint; R0 scheduled run 36335205678 succeeded on descendant 1cca43e4137a9996fd1b533c298bc20f10f897be. SCIM any-row rule retained. DoD 2 not applicable: no Dockerfile, goreleaser or image change. DoD 3 not applicable: no new signal or attribute.
<!-- SECTION:NOTES:END -->
