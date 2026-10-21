---
id: CFO-0089
title: >-
  aigateway.logs: take bodies from the log detail and stop listing at the commit
  budget
status: In Progress
assignee:
  - '@claude'
created_date: '2026-10-21 03:13'
updated_date: '2026-10-21 03:30'
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
- [ ] #1 With complete heads, each captured log costs exactly one REST read (the detail GET); no /request or /response GET is made
- [ ] #2 Budgeted collection stops listing at a bounded row count and returns a whole-second mark, and consecutive calls deliver every row exactly once across same-second page boundaries
- [ ] #3 Live: in one fenced epoch, aigateway.logs REST requests per exported log is at most 1.2 over a completed poll
- [ ] #4 A side whose head is incomplete, missing or not a JSON string falls back to its own raw body read, so usage recovery and non-JSON classification stay exact; the other side's complete head is still used
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Red tests: one detail read per log with complete heads; incomplete head becomes a truncated prefix with no usage recovery or non-JSON count; null head is unavailable; budgeted listing stops at a row cap with an exactly-once boundary across same-second pages. 2. fetchLog: single detail GET, decode head JSON strings, carry completeness; emit forces truncated for incomplete sides. 3. CollectWindowBudget: page per gateway until a row cap, limit the window to the earliest gateway's last complete second, emit within it. 4. Update fixtures that model heads as header objects to the live string shape, keep the header-redaction test. 5. Focused race tests, gaterun just check, CodeRabbit, push, then live calls-per-log readback.

Revised step 2: a head replaces a body read only when it is a complete JSON string; anything else keeps that side's raw read (404 code 7002 still marks it unavailable). Chosen over exporting incomplete prefixes because it keeps response-body usage recovery exact for rare bodies over the ~2 MB head cap at the cost of one extra read for those logs only.
<!-- SECTION:PLAN:END -->
