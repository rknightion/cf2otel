---
id: CFO-0089
title: >-
  aigateway.logs: take bodies from the log detail and stop listing at the commit
  budget
status: Done
assignee:
  - '@claude'
created_date: 2026-10-21 03:13
updated_date: '2026-11-06T00:03:00Z'
labels:
  - bug
dependencies: []
priority: high
ordinal: 119000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Each captured AI Gateway log costs three REST reads (detail, /request, /response) plus re-listed pages: 3.86 calls per exported log live on 2026-10-21, against about 2.4 rps left for aigateway.logs under the 4 rps per-token Cloudflare limit, so capture tops out near 1950 logs/h and fell behind the jg spike (see CFO-0085). Live, the detail GET already returns request_head and response_head as body text (JSON strings, not header objects) with request_head_complete/response_head_complete; 20 of 20 sampled logs had both complete and a complete head matched the raw /request body byte-for-byte. Exported content is capped at ai_gateway.max_body_bytes (default 16 KiB), far below the ~2 MB head cap, so the body GETs buy nothing but exact non-JSON classification and usage recovery on non-streamed JSON responses over the head cap. Separately, a budget-truncated window lists every page to the window end before committing about 16 MB, then the next window re-lists the same pages.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 With complete heads, each captured log costs exactly one REST read (the detail GET); no /request or /response GET is made
- [x] #2 Budgeted collection stops listing at a bounded row count and returns a whole-second mark, and consecutive calls deliver every row exactly once across same-second page boundaries
- [x] #3 Live: in one fenced epoch, aigateway.logs REST requests per exported log is at most 1.2 over a completed poll
- [x] #4 A side whose head is incomplete, missing or not a JSON string falls back to its own raw body read, so usage recovery and non-JSON classification stay exact; the other side's complete head is still used
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Red tests: one detail read per log with complete heads; incomplete head becomes a truncated prefix with no usage recovery or non-JSON count; null head is unavailable; budgeted listing stops at a row cap with an exactly-once boundary across same-second pages. 2. fetchLog: single detail GET, decode head JSON strings, carry completeness; emit forces truncated for incomplete sides. 3. CollectWindowBudget: page per gateway until a row cap, limit the window to the earliest gateway's last complete second, emit within it. 4. Update fixtures that model heads as header objects to the live string shape, keep the header-redaction test. 5. Focused race tests, gaterun just check, CodeRabbit, push, then live calls-per-log readback.

Revised step 2: a head replaces a body read only when it is a complete JSON string; anything else keeps that side's raw read (404 code 7002 still marks it unavailable). Chosen over exporting incomplete prefixes because it keeps response-body usage recovery exact for rare bodies over the ~2 MB head cap at the cost of one extra read for those logs only.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in 8e62766 (pushed). Red first: complete-head test read both bodies; incomplete-head test read both; listing test listed 42 pages in one call against a 24-page bound. fetchLog now GETs the detail, takes a body from a complete JSON-string head, and raw-reads only the remaining sides concurrently with joined cancellation. Budgeted CollectWindowBudget pages per gateway, emits rows before the earliest gateway's last fully listed second, and returns once listRowCap (500) rows are listed; the unbudgeted path still lists everything first. A page whose new row precedes the horizon fails the window instead of skipping it. The two overlap tests were rewritten for detail-first ordering. Package race tests pass; remote gate run 20261021T032544Z-cf2otel-999c49e1 just check exit 0; CodeRabbit complete, 1 minor finding (AC wording) fixed via CLI. AC1, AC2 and AC4 are covered by tests; AC3 (live calls per log at most 1.2) awaits a completed poll on the deployed build.

Live readback, epoch 0.19.0-rc.2 (commit 8e62766, process start 1792554303, one instance): first aigateway.logs poll succeeded in 1186s, 4 commits (catch-up windows 3), 1304 rows exported for 1621 aigateway.logs REST calls = 1.24 calls per row (was 3.86). The call count includes the next poll's in-flight reads, so the per-commit figure is lower, but AC3 (at most 1.2) is NOT checked: no clean per-poll split was observed. Capture is now about 3950 rows/h (was about 1950): aigateway.logs used about 1.4 rps, so it is latency-bound (one sequential detail GET of up to ~4 MB per log), not quota-bound. The checkpoint still ages while the cursor is inside the 10000-15000/h block of 2026-10-20 00:00-01:00. The earlier :main epoch's two failed polls were not diagnosable: its container logs were discarded by the Watchtower recreate (CFO-0090).

Revalidated source behavior during AI poller performance work: focused race checks cover complete-head reads, raw fallback, ordered overlap and dense budget delivery; full AI Gateway race suite passed in 194.094s. Source criteria 1,2,4 now checked. Live calls-per-record criterion remains open; recent deployment epochs restarted before a clean completed-poll measurement.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Live completed-poll evidence now satisfies the remaining calls-per-record criterion. Release0.20.1 commit153f5313811341526adaa52ceb718362c653d066, process start1793921557.2273192: first completed successful AI poll committed64 source spans with75 physical requests(1.172 per record); two completed polls total103 records/118 requests(1.146). These are poll-completed diagnostics, not cumulative counters contaminated by the next in-flight poll. Source regression and final composed just check passed; release CI42129579022 passed. Local receipts: /Users/rob/repos/cf2otel/codex/cfo92-release-impact-live.json (first valid poll, later host-check failure retained) and /Users/rob/repos/cf2otel/codex/cfo92-release-telemetry-impact.json (continuation). No packaging changes or new signals specific to this original task. Sustained alert/throughput observation remains under CFO-0092 (AI polling slow/backlog alerts).
<!-- SECTION:FINAL_SUMMARY:END -->
