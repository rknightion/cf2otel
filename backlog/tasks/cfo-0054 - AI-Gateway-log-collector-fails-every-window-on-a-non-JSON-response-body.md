---
id: CFO-0054
title: AI Gateway log collector fails every window on a non-JSON response body
status: Done
assignee:
  - loop14-root
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 13:55'
labels:
  - aigateway
dependencies: []
priority: high
type: bug
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Since 2026-09-27 the aigateway.logs collector has failed every run (about 288 failures a day) with "aigateway response body: invalid character R looking for beginning of value": a body fetch returns a non-JSON payload and the error fails the whole window, so no AI Gateway logs, spans or metrics have been exported for over three days and the checkpoint is not advancing. The collector-stale alert has been firing since then. How far back the log API reaches decides how much of the gap can still be recovered.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A reproduction test with a non-JSON body response fails before the fix
- [x] #2 A non-JSON or error body is recorded on that request (and counted) without failing the window; metadata-only export continues
- [x] #3 After deploy the collector catches up and its stale alert clears; the unrecoverable span, if any, is recorded
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 candidate 14ef1dbbaccbc56a7f7a74b6297b3cd67a78af74 independently reviewed PASS on that exact SHA. Non-JSON body assertions fail on base and pass candidate; just check and CodeRabbit complete, zero findings/all seven files covered. Root landing 67eee5828089708ae25544b43d77d6fe89022c88 passed gen-check/check, leak scan zero hits, pushed; CI and deployed catch-up/stale-alert AC3 pending.

DEP3 v0.10.1 passed the full ten-minute health check with exact image index and 40 monotonic prior checkpoint keys. LIVE54 terminal PASS at 2026-10-01 13:53:48 UTC: two reads advanced the AI Gateway checkpoint from September 28 21:10:57 to 22:10:57; last-success ages 312 and 324 seconds; stale instance Normal on both reads; 100 request-event timestamps inside the outage interval were present in Loki. This meets the frozen advancing-catch-up proof, not a claim that the full backlog is drained or every outage event recovered. No unrecoverable boundary was established: outage records are present, so no gap is inferred from absence. DEP2 earlier 60-minute watch failed, followed by late v0.10.0 recovery before the adaptive correction was deployed; that history is retained and recovery is not exclusively attributed to subdivision. DoD2 conditional image-packaging changes do not apply to the source fixes; stable release publication evidence is separate.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Non-JSON bodies no longer fail an entire window: metadata-only records continue with omission flags and counting. The subsequent aggregate delivery-budget limitation was repaired through opt-in adaptive source-window subdivision without changing the 90-second budget or capture limits. Independent exact-SHA reviews, gates, CodeRabbit, CI and deployed advancing-checkpoint/freshness/Normal-alert proof passed. Catch-up remains ongoing; complete outage recovery is not claimed.
<!-- SECTION:FINAL_SUMMARY:END -->
