---
id: CFO-0032
title: Extend the API drift canary contract to every built dataset
status: To Do
assignee: []
created_date: '2026-09-25 12:07'
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
- [ ] #1 contract.json has an entry with required fields and minimum maxDuration and notOlderThan for every dataset and REST path a registered collector queries, and a unit test fails when a registered collector's dataset has no entry
- [ ] #2 A scheduled canary run succeeds at the exact SHA and its logs and artifacts contain zero token bytes
- [ ] #3 Any schema-probe token permission change is read-only, recorded by permission-group name before and after, and touches no other token
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
