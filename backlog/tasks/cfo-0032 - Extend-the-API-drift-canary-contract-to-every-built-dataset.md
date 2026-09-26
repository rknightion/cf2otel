---
id: CFO-0032
title: Extend the API drift canary contract to every built dataset
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 12:13'
labels: []
dependencies:
  - CFO-0024
priority: medium
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
spec/cloudflare/contract.json checks 4 GraphQL datasets and 4 REST paths, so the canary does not watch firewall, DNS analytics, RUM, Gateway DNS, audit v2, SCIM, the 22 platform Groups datasets or email. The canary exists to catch the one-unentitled-field-fails-the-query trap. Rob authorised (2026-09-25) widening the schema-probe token with additional read-only permission groups where a dataset cannot otherwise be introspected; that token only.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 contract.json has an entry with required fields and minimum maxDuration and notOlderThan for every dataset and REST path a registered collector queries, and a unit test fails when a registered collector's dataset has no entry
- [x] #2 A scheduled canary run succeeds at the exact SHA and its logs and artifacts contain zero token bytes
- [x] #3 Any schema-probe token permission change is read-only, recorded by permission-group name before and after, and touches no other token
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7: contract and AST coverage test landed at 1daf4be; local live canary matched after the bounded firewall/detailed-log correction, just ci passed, and CI 36237411272 had ci-success success. TOKEN-32 added only Access Audit Logs Read, Access SCIM Logs Read, AI Gateway Read and Account Settings Read to the schema-probe token in one PUT, re-GET confirmed the read-only groups and unchanged resources; no runtime token edit. AC2 remains open until the first scheduled Cloudflare API drift run created after both the token edit and 1daf4be push concludes, with artifact/log token-byte inspection.

Loop 7 AC2: first scheduled Cloudflare API drift run after the read-only schema-token edit and L32 push was run 36238824560, created 2026-09-26T11:26:28Z at exact SHA eb5c2c7 (contains L32). The probe job succeeded and its log said Cloudflare API contract matched. The workflow produced zero artifacts; the downloaded run log and artifact directory were scanned against exact bytes of five locally held credentials, including the schema-probe token, with zero hits. No token bytes entered tracked files.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Expanded the drift contract to all registered collector API surfaces, added a coverage test, widened only the schema-probe token with four read-only groups, and verified the first post-change scheduled canary and token-byte scan.
<!-- SECTION:FINAL_SUMMARY:END -->
