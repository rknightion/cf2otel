---
id: CFO-0069
title: >-
  AI Gateway log delivery stalls again on trace export deadlines after adaptive
  window halving
status: In Progress
assignee: []
created_date: '2026-10-01 17:50'
updated_date: '2026-10-01 18:28'
labels:
  - aigateway
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On 0.10.1 the aigateway.logs collector delivered for about 90 minutes after deploy and then stopped: no success since about 15:10Z on 2026-10-01, with "aggregate commit budget exhausted" errors as the source window halved from 15m to 7m30s to 3m45s (traces: context deadline exceeded) at roughly one failure per fifteen to thirty minutes and no success in between. Loop 14 accepted the fix on a two-read checkpoint advance thirteen minutes after deploy. The exclusive cause of the OTLP trace export deadline was never proven; the aggregate 90-second commit budget, per-span body size and the trace endpoint's own limits are all candidates. Halving one step per failed cycle converges too slowly to be a recovery mechanism, and the stale-collector alert fires again.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The cause of the trace export deadline is established from live evidence (per-commit span count, bytes and exporter response), not inferred from the timeout
- [x] #2 A reproduction through the scheduler, provider and a real HTTP OTLP collaborator fails by assertion on the released code
- [x] #3 A window whose trace payload cannot be delivered inside the commit budget converges to a deliverable size within one scheduled cycle, exporting before the checkpoint moves and without dropping or truncating required signals silently
- [ ] #4 After deploy the collector's last-success age stays under 15 minutes for 60 continuous minutes and the stale alert instance is Normal
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Cause measured live: one source burst of about 1,340 gateway rows in four minutes with roughly 90 KB bodies each. A window that misses the commit deadline is collected again from its start, re-reading every body (about 1.5 s per row), the window halves once per attempt, and the reduced window never grows back, so four reduced windows per run cover less source time than elapses.

2. Bound one commit by payload instead of source interval: a budgeted collector emits whole source seconds until the buffered records reach the budget and returns that second as its mark; the scheduler keeps committing truncated slices inside the run.

3. On an aggregate deadline halve the payload budget (floor one export chunk); double it back once per quiet hour.

4. Reproduction through the scheduler and the registered collector; the real-HTTP delivery test updated to the payload-budget contract.

5. Release, deploy and a 60-minute sustained live read.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Preparation fix 58ad345: aigateway.logs is a budgeted collector. It emits whole source seconds until the buffered records reach a 16 MiB payload budget and returns that second as its mark; the scheduler commits the slices inside one run, halves the budget on an aggregate deadline (floor one 512 KiB export chunk) and doubles it back once per quiet hour. Cause measured live: a burst of 1,340 gateway rows in four source minutes at about 124 KB exported per row and 1.5 to 1.7 s of body fetches per row; each failed window was collected again from its start, the window halved once per attempt and never grew back. Reproduction TestDenseBurstAdvancesWithinOneRun failed on unchanged behaviour with the live error text and passes. TestRegisterAdaptiveDeliveryBudget was changed because the intended behaviour changed: it asserted two failed window halvings and now asserts budget halving, unchanged checkpoint and metrics on a failed slice, and complete exactly-once delivery. just check exit 0; CodeRabbit 0 findings over all 7 files. AC1 to AC3 are met by this evidence; AC4 needs the sustained live read after deploy.
<!-- SECTION:NOTES:END -->
