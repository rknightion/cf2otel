---
id: CFO-0041
title: Accept domainless Worker destinations in Access apps drift contract
status: Parked
assignee: []
created_date: '2026-09-26 20:10'
labels:
  - access
  - api-drift
dependencies: []
priority: low
type: bug
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop 9 R0 run 36268367867 failed the existing REST access-apps contract: scope #1 missing field domain. A read-only live shape check found 17 Access apps: 15 with domain and two self_hosted apps without domain. The two have destinations of types worker and all_preview_workers. The current canary validates only the first row and requires domain unconditionally, so its red result does not establish an API regression. This task needs an owner decision on the accepted conditional shape before work is admitted.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Access apps contract accepts the live documented Worker destination shapes without a domain while still requiring domain for domain-based apps
- [ ] #2 A regression test covers domain-based and domainless Worker rows without relying on result order
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
