---
id: CFO-0041
title: Accept domainless Worker destinations in Access apps drift contract
status: Done
assignee: []
created_date: '2026-09-26 20:10'
updated_date: '2026-09-27 17:03'
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
- [x] #1 Access apps contract accepts the live documented Worker destination shapes without a domain while still requiring domain for domain-based apps
- [x] #2 A regression test covers domain-based and domainless Worker rows without relying on result order
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 9 R0: 0/4 implementation attempts, 0/3 review cycles, 0 infrastructure retries. The single authorized drift canary failed on an Access Worker destination row without domain; a read-only census found 15 domain-based and two domainless Worker destination apps. No source mutation was admitted. Owner decision remains the resume condition.

Loop 10: implementation attempt L41-a1 used (1/4); review-repair 0/3; infrastructure retries 0. Retained candidate a3c419003b13438f1ecdc090d154b24daa4cce80 passed just check and CodeRabbit reviewed all 3 changed files, but CodeRabbit major and independent REV-41 FAIL both found access-apps still requests per_page=1. A later domainless public or mixed-destination app is never returned, so all-row validation cannot prove AC2. Candidate was not landed; R0 was not dispatched. Frozen lane scope forbids changing pages/query. Resume after owner amends that constraint, then repair with a query-aware behavioral red test, fresh CodeRabbit and independent PASS on the exact candidate.

Loop 11: implementation L41-a2 used (2/4); review-repair 0/3; infrastructure retries 1 CodeRabbit socket close before complete. Grant: Access paging repair from retained a3c4190. Candidate commit fcc981f4eeafd14731e642b4bdc1ba34e517d30c passed exact-SHA just check and CodeRabbit complete (3 files, no unreviewed); red-then-green query-aware tests. Not landed: mandatory independent REV-C was not dispatched after a root wait/dispatch failure. Resume with REV-C PASS on this exact SHA, pre-land live check zero differences, landing scan/CI, then R0 success; retain l11-lc.

Loop 12: implementation 2/4 (L41-a1, L41-a2); review-repair 1/3 (L41-rr1); infrastructure retries 1 carried from loop 11, 0 new. Grant: exact-SHA review, narrow null-rule repair, main landing and read-only canary. REV-C2 PASS on 661d3a7014dd166adece9b4f386d90eef3b87459; pre-land live contract matched; merge 4c951399680d8dc50725da7df94b0ec3ceb42dc4 passed just check and all eight workflows, including CI ci-success and actionlint; R0 scheduled run 36335205678 succeeded on descendant 1cca43e4137a9996fd1b533c298bc20f10f897be. DoD 2 not applicable: no Dockerfile, goreleaser or image change. DoD 3 not applicable: no new signal or attribute.
<!-- SECTION:NOTES:END -->
