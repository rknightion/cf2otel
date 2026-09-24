---
id: CFO-0017
title: Account audit logs collector (v2)
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 08:18'
labels:
  - 'wave:2'
  - audit
dependencies: []
priority: medium
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
/accounts/{a}/logs/audit with cursor pagination and boundary dedupe by event id (doc-0004).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Parity with doc-0004 audit section
- [x] #2 Live verification on the m7kni stack
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: implement cursor-paginated audit v2 collector and verify records live.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Audit v2 cursor pagination and boundary dedupe verified by focused tests, just check and just ci at a3fa01b. Live audit event and metric observations are being reconciled for AC2.

Live discrepancy at v0.2.2: audit.logs checkpoint reached 17:36:09Z. A fresh read-only source census for 2026-09-23 10:00-17:59Z found 293,241,261,112,16,87,205,256 IDs per hour; matching Loki rows were 147,0,261,0,0,0,0,0. A 15:08:55Z source ID is absent from a 30h Loki query despite the checkpoint passing its window; a collector-equivalent 14:36:08-15:36:09Z source query now returns that ID among 26 rows. Cause may be late provider availability or a collector query/flush issue; not established. AC1 parity and AC2 live completeness remain open. No checkpoint rewind is authorised by goal section 5.

Correction after validating Loki structured-metadata filtering: the earlier event_name-only query undercounted. With {service_name="cf2otel"} | event_name="cloudflare.audit.event" | cloudflare_audit_id=~".+", 2026-09-23 10-17 UTC source/Loki hourly counts are 293/293, 241/241, 261/261, 112/0, 16/8, 87/0, 205/69, 256/0. An exact 15:08:55 UTC source ID remains absent from a wide Loki search despite a checkpoint past its window. Live m7kni stack observation is 538 audit log rows in a six-hour query and 31 cloudflare_audit_events_total series over ten hours. AC2 confirms signal presence only; AC1 parity stays open. No checkpoint rewind was authorised.

Read-only gap probe on 2026-09-23: the missing 15:08:55 UTC source event is now returned by both a 15:00-16:00 UTC query and a narrow 15:04:59-15:10:00 UTC query (one matching ID in one page for each). This rules out a present-day narrow-window filter mismatch for that ID, but does not establish when it first became available. The collector checkpoint had already passed it and Loki still lacked it; possible delayed source visibility remains unproven. Do not rewind the live checkpoint under wave 2 authority. AC1 remains open pending a safe replay and source-lag investigation.

Wave 2 park boundary: source/Loki hourly parity was 293/293, 241/241, 261/261, 112/0, 16/8, 87/0, 205/69, 256/0 for 10:00-18:00 UTC on 2026-09-23. One exact source event at 15:08:55 UTC is absent in Loki after the checkpoint passed it. Resume by investigating source visibility lag and implementing a safe overlap/replay strategy with dedupe, then prove parity in a fresh live window. Current checkpoint must not be rewound ad hoc.

Loop 3 v0.3.1 forward audit proof: measured source visibility lag on 369 fresh events was 167-450 seconds, so collector holdback increased from 2 to 10 minutes without checkpoint rewind or backfill. First complete post-fix hour [2026-09-24 07:00,08:00) UTC reached checkpoint 08:04:09Z and matched exactly: source 248 IDs, Loki 248 rows/248 IDs, zero missing, extra or duplicates, query limit not hit. The required second complete nonzero hour [08:00,09:00) cannot mature before the 08:40 closeout drain bound; AC1 remains open. Resume after audit.logs passes 09:00Z, run fresh source/Loki per-ID parity for that hour, and check AC1 only if both hours match.
<!-- SECTION:NOTES:END -->
