---
id: CFO-0044
title: >-
  Treat JSON null as missing in the conditional Access domain and SCIM any-row
  drift checks
status: To Do
assignee: []
created_date: '2026-09-27 18:30'
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
- [ ] #1 A row whose `domain` is JSON null and whose destinations are not all `worker`/`all_preview_workers` is reported missing; a Worker-destination row with null `domain` stays exempt
- [ ] #2 A nonempty SCIM page whose only `resource_user_email` values are JSON null reports the field absent from all rows; one non-null value on any row satisfies it
- [ ] #3 Fields in plain `required_fields` keep null-as-present, pinned by a test
- [ ] #4 Each new test fails by assertion on the pre-change base and passes after, and the canary matches the live API at the landed SHA
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
