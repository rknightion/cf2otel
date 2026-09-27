---
id: CFO-0044
title: >-
  Treat JSON null as missing in the conditional Access domain and SCIM any-row
  drift checks
status: Done
assignee: []
created_date: '2026-09-27 18:30'
updated_date: '2026-09-27 19:26'
labels:
  - access
  - api-drift
dependencies: []
priority: low
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop 12 review (REV-C2) found that the drift canary counts a present-but-null field as present. Two frozen owner rules depend on the field carrying a value: Access apps `domain` is required except on Worker-destination rows (Rob, loop 10), and SCIM `resource_user_email` must appear in at least one row of a nonempty page (Rob, 2026-09-27). If Cloudflare sent explicit nulls, a domain-based app with `"domain": null` would pass and an all-null SCIM page would pass, masking the group-only page the owner chose to flag. Owner decision (Rob, 2026-09-27, loop 13 preparation): null counts as missing, scoped to fields named in `optional_when_destination_types` and `required_in_any_row`; plain `required_fields` checks keep null-as-present. Empty strings are out of scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A row whose `domain` is JSON null and whose destinations are not all `worker`/`all_preview_workers` is reported missing; a Worker-destination row with null `domain` stays exempt
- [x] #2 A nonempty SCIM page whose only `resource_user_email` values are JSON null reports the field absent from all rows; one non-null value on any row satisfies it
- [x] #3 Fields in plain `required_fields` keep null-as-present, pinned by a test
- [x] #4 The AC1 and AC2 tests fail by assertion on the pre-change base and pass after; the canary matches the live API at the landed SHA
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 13: L44-a1 (1 implementation attempt), 0 review-repair attempts, 0 infrastructure retries. Grant: scoped null rule in tools/apidrift/probe.go and probe_test.go; reviewed candidate 0aba54402c31a9c0fabb50ca4339a57169229e66. AC1/AC2 failed by assertion on base 14782d2d33000e0e8c5609a45182b711d2c9b19f and passed on candidate; REV PASS; pre-land API contract matched; landing just check and eight workflows succeeded at 7986f331c0567e41eb18cb13089724f101e55736; R0 run 36344241272 probe succeeded. DoD 2 and 3 not applicable: no Dockerfile, goreleaser, image, signal or attribute change.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Scoped JSON null handling for conditional Access domain and SCIM any-row checks. Base assertion witnesses, independent exact-SHA review, just check, landing CI and live R0 passed.
<!-- SECTION:FINAL_SUMMARY:END -->
