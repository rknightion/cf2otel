---
id: CFO-0024
title: 'Email routing, email sending and DMARC report datasets'
status: Parked
assignee:
  - '@rknightion'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-26 17:06'
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
- [x] #2 Collectors for the Groups-backed email datasets E24-A confirmed emit only frozen semconv names and bounded attributes, with tests and docs rows
- [x] #3 A release containing them is deployed to camden and healthy
- [ ] #4 Each built email dataset is proven live under the same rule as CFO-0023 AC4
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 6: repair independent review blockers, re-review, land exact SHA, release, deploy and compare email Groups source counts with Mimir.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 5 E24-A: fresh 30-day GraphQL census across 23 currently listed zones, ending 2026-09-25 UTC, found routing raw on 2 zones, sending raw on 2, DMARC raw on 0; emailRoutingAdaptiveGroups and emailSendingAdaptiveGroups both exist, are enabled, advertise count and datetime fields, and have rows on 2 zones each. Private per-zone evidence: codex/samples/email_dataset_presence_loop5.json; schema: codex/samples/email_schema_loop5.json. Build only the two Groups-backed families after INT-23; skip DMARC. Loop 5 attempts: implementation 0/4, review-repair 0/3, infrastructure retries 0, grant: frozen CFO-0024 decision; AC1 proven.

Loop 5 E24-B local candidate a2c5ba8486a9fc995113543d35b43559e269107e is committed but not pushed, preserving CFO-0023's first release cycle. Both Groups collectors aggregate only account-owned zones; DMARC omitted. Exact-SHA just check passed; CodeRabbit first review found a retention-gap scheduler defect, reproduced red and fixed, and the correction review completed with zero findings. Implementation attempts: worker 1/4 plus one root lint/retention repair; review-repair 1/3; infrastructure retries 0; grant: frozen CFO-0024 decision. AC2 stays unchecked until the candidate is pushed with its docs; AC3/4 require its later release, Camden deploy and live source-to-Mimir proof. Resume after CFO-0023 LIVE-23 completes or parks and the required review/release route is available.

Loop 5 owner closeout: E24-B routing and sending Groups candidate 54cb82708afa0ad441b807f290fb1121592b534d is committed locally only, with docs, exact-SHA just check and just ci passing and CodeRabbit correction review complete. Required independent REV-E24-R2 has no verdict; no email commit was pushed, released or deployed, so AC2-4 remain unchecked. Resume by reviewing that exact candidate against 182b285d6b24e2d683ac837c5ac324d312a4dcc1, then reconcile with current main without discarding the local candidate. Implementation attempts worker 1/4 plus root corrections; review-repair 3/3 used (retention error, fail-closed zone selection, dated test fixture); infrastructure retry: review-dispatch stall; grant: frozen build routing and sending only, no DMARC.

Loop 6: candidate 9bb6ff8 passed independent email review; merged and pushed cbc3d0de6a68daf5d645f5613ceb4cb0cde77bf5. just check and just ci passed at exact SHA; nine workflow runs concluded success. Added-line privacy scan found zero hits across 23 zones. AC3 and AC4 still require a release, Camden deployment and source-to-Mimir comparison.

Loop 6 release gate parked: at DEP-ready after P8, release-please PR #20 base 7a7bbdd and head 29000c9 proposed v0.5.2 but its generated CHANGELOG omitted the landed Email Routing and Sending feature a4aeb08 and earlier email fixes reachable since v0.5.1. The PR was not merged. CFO-0035 tracks the release-note repair. Resume only when a regenerated release candidate at a named main SHA lists the feature and newly delivered fixes and all required workflows at that SHA pass; then REL-n, Camden DEP-n and LIVE-24 source-to-Mimir proof remain. AC2 stays proven; AC3/4 are unchecked.

Loop 7: Release v0.6.0 at merge eb5c2c7 contains the Email Routing and Sending Groups collectors. Camden DEP-1 pinned 0.6.0; its full ten-minute health watcher ended healthy at 2026-09-26T11:40:43Z. Running image RepoDigest matched release index sha256:189929bc183cf483dcd49991383fbf7e72a1040dd4872eba669d7c94f58beed5; all 36 preexisting checkpoint keys were monotonic, email.routing and email.sending appeared, config hash was unchanged, and 69 startup log lines had no registration, field-limit or selection errors. AC4 awaits exact source-to-Mimir proof or the frozen absent-input adjudication.

Loop 7 run-end park: implementation attempts 3/4 and review-repair 5/7 carried, no new implementation or review attempt; infrastructure retries 0; grant: v0.6.0 deploy and one three-hour absent-input extension. AC3 passed after healthy Camden DEP-1. First complete [11:30Z,11:40Z) source window had zero routing and zero sending rows/count on 23 enabled owned zones; prior seven days had a routing row but no sending row. The extended checkpoint target is 2026-09-26T14:30:00Z with root watcher deadline 15:04Z; terminal receipt codex/live-loop7/checkpoint-ready-3h.json was absent at 14:37Z. AC4 remains unchecked; on a ready receipt, query both Groups datasets over [11:30Z,14:30Z), compare any nonzero source exactly with Mimir after delivery, and adjudicate zero input under ship.md. If receipt times out, preserve as pending and repeat from a fresh complete window. No source-to-Mimir equality was claimed.

Loop 7 post-watcher correction: the extended watcher reached both email checkpoints at 14:30Z and finished its delivery wait at 14:43:02Z, after the earlier 14:37Z park note. The exact [11:30Z,14:30Z) Groups source census across 23 enabled owned zones found Routing 13 in six rows and Sending 0 in zero rows; Sending also had no same-selection row in the prior seven days. At 15:00Z Mimir showed one routing counter series value 16 and one sending series value 0. The routing counter includes startup backfill: source [11:00Z,14:30Z) was 16 in eight rows, equal to the observed cumulative counter 16; source [11:20Z,14:30Z) was 14. Therefore the 13 vs 16 difference is window provenance, not claimed same-window equality. Sending remains a defect candidate under ship.md and AC4 stays unchecked. Resume with FIX-E24 on a sanitized failing fixture and independent REV-E24, within remaining implementation attempt 4/4 and review-repair rounds 6-7/7, then release/deploy/live proof; do not treat the zero source as acceptance. Parked status remains.

Loop 8 preparation re-grade 2026-09-26: loop 7's 'defect candidate' reading of the Email Sending zero is withdrawn. The source was 0 and Mimir's sending counter was 0, which agree; there is no failing case to build a FIX-E24 fixture from, and loop 5's 30-day census had sending rows. This is absent input under the 7-day rule, not a code defect. Rob, 2026-09-26: the loop sends real email through Cloudflare Email Sending (account key authorised; a short-lived scoped Email Sending token is acceptable) to create source rows, ideally to an address on a routing-enabled zone so Routing also records it. Then repeat source-to-Mimir in a fresh window. No FIX-E24 without a real mismatch. Implementation attempts stay 3/4 and review-repair 5/7.

Loop 8 LIVE-24b (2026-09-26): two real Email Sending REST sends (POST /accounts/{account}/email/sending/send, fictional content, sender on an account sending subdomain, recipient a mailbox Rob owns) at 16:19:29Z and 16:21:30Z, both HTTP 200 success, account-key auth accepted (no token minted). Window [16:15Z,16:35Z) after both email checkpoints passed 16:35Z (16:45:46Z) plus 2-min delivery: Routing EXACT: source emailRoutingAdaptiveGroups count 4 (two five-minute buckets, 2+2) across 23 enabled owned zones, baseline [15:50Z,16:15Z) 0; Mimir cloudflare_email_routing_events_total one series flat 18 until the window commits, then 20 and 22 (+4). Sending: emailSendingAdaptiveGroups 0 rows in all 23 zones at 16:49Z and again at 17:05Z; the raw emailSendingAdaptive dataset also had 0 rows in any zone over [16:00Z,17:10Z); Mimir cloudflare_email_sending_events_total one series 0. Real REST API sends did not reach either sending dataset within 50 minutes, so the sending collector has no live input to prove; no FIX (source 0 = Mimir 0). AC4 stays open for sending only. Resume: a send path that populates emailSendingAdaptive (e.g. a Workers send_email binding or SMTP send), or Cloudflare confirmation of which sends the dataset records. Loop 8: implementation 3/4 (unchanged), review-repair 5/7 (unchanged), infrastructure retries 0, grant: up to 5 sends (2 used); reason parked on absent sending source input.
<!-- SECTION:NOTES:END -->
