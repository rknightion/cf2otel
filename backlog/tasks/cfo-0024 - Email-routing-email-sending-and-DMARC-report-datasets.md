---
id: CFO-0024
title: 'Email routing, email sending and DMARC report datasets'
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-25 10:17'
labels:
  - 'wave:2'
  - email
dependencies: []
priority: low
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Zone datasets emailRoutingAdaptive, emailSendingAdaptive, dmarcReportsAdaptive.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Check which zones have data before building
- [ ] #2 Collectors for the Groups-backed email datasets E24-A confirmed emit only frozen semconv names and bounded attributes, with tests and docs rows
- [ ] #3 A release containing them is deployed to camden and healthy
- [ ] #4 Each built email dataset is proven live under the same rule as CFO-0023 AC4
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 5 E24-A: fresh 30-day GraphQL census across 23 currently listed zones, ending 2026-09-25 UTC, found routing raw on 2 zones, sending raw on 2, DMARC raw on 0; emailRoutingAdaptiveGroups and emailSendingAdaptiveGroups both exist, are enabled, advertise count and datetime fields, and have rows on 2 zones each. Private per-zone evidence: codex/samples/email_dataset_presence_loop5.json; schema: codex/samples/email_schema_loop5.json. Build only the two Groups-backed families after INT-23; skip DMARC. Loop 5 attempts: implementation 0/4, review-repair 0/3, infrastructure retries 0, grant: frozen CFO-0024 decision; AC1 proven.

Loop 5 E24-B local candidate a2c5ba8486a9fc995113543d35b43559e269107e is committed but not pushed, preserving CFO-0023's first release cycle. Both Groups collectors aggregate only account-owned zones; DMARC omitted. Exact-SHA just check passed; CodeRabbit first review found a retention-gap scheduler defect, reproduced red and fixed, and the correction review completed with zero findings. Implementation attempts: worker 1/4 plus one root lint/retention repair; review-repair 1/3; infrastructure retries 0; grant: frozen CFO-0024 decision. AC2 stays unchecked until the candidate is pushed with its docs; AC3/4 require its later release, Camden deploy and live source-to-Mimir proof. Resume after CFO-0023 LIVE-23 completes or parks and the required review/release route is available.

Loop 5 owner closeout: E24-B routing and sending Groups candidate 54cb82708afa0ad441b807f290fb1121592b534d is committed locally only, with docs, exact-SHA just check and just ci passing and CodeRabbit correction review complete. Required independent REV-E24-R2 has no verdict; no email commit was pushed, released or deployed, so AC2-4 remain unchecked. Resume by reviewing that exact candidate against 182b285d6b24e2d683ac837c5ac324d312a4dcc1, then reconcile with current main without discarding the local candidate. Implementation attempts worker 1/4 plus root corrections; review-repair 3/3 used (retention error, fail-closed zone selection, dated test fixture); infrastructure retry: review-dispatch stall; grant: frozen build routing and sending only, no DMARC.
<!-- SECTION:NOTES:END -->
