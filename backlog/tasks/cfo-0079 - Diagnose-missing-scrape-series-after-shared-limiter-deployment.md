---
id: CFO-0079
title: Diagnose missing scrape series after shared-limiter deployment
status: In Progress
assignee:
  - '@loop18-root'
created_date: '2026-10-03 15:23'
updated_date: '2026-10-04 12:56'
labels:
  - ops
  - limiter
dependencies: []
priority: high
type: bug
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop17 deployed0.16.1 with default shared0.5rps/burst1, process remained healthy with retained checkpoints. HOUR2 at15:05:21Z and15:10:21Z stopped after2reads: collector count39 versus49expected; healthchecks.events attempts/success series missing and last-success timestamp stillzero. This is observed incomplete/never-successful scrape coverage, not yet an attributed limiter defect or infrastructure failure. One unchanged49collector sustained-hour retry is running; no healthy deploy reverted. First0.16.0 hour passedall49. Preserve firstreceipt and diagnose exact missing collectors, queue/context budgets and actual safe errors before source/config changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Missing or never-successful collectors and their precise cause are attributed with sanitized timestamped evidence
- [ ] #2 Any required correction has an authentic registered-collector or public-client reproduction and candidate pass without weakening49collector coverage or timeout/window guarantees
- [x] #3 A deployed sustained-hour proof shows attempts equal successes for affected collectors, or remaining live criteria are parked with both receipts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Read-only root diagnosis mapping against deployed limiter source and first/retry-hour receipts, safe current Mimir poll/error/age samples and sanitized host logs. No new implementation budget or configuration write granted; affected live criteria park after the one authorized retry if still failing.

Loop18: D1 source/live quota design packet, independent design review, F1 registered-collector red/green correction with security review and CodeRabbit before landing, composed gate and exact-SHA CI, release/deploy ops, all first polls then unchanged 65-minute hour and interval-age closeout.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 D79 read-only mapping attributes hour-witness failure to incomplete completed-poll coverage, not demonstrated error or starvation. Poll counters publish only after RunOnce returns. At15:29:32Z46completion series, all49error serieszero, three names email.routing/firewall.metrics/httpreq.metrics have lastsuccesszero;1064APIrequests compatible with shared0.5rps pacing but no percollector queue/inflight proof. Both HOUR2 and its only retry failed after2reads due count39/49 then46/49, all present attempt/success counts match. Root started before direct proof of all49first cycles, an avoidable precondition miss, not new source evidence. No rate/burst/config/source change or budget reset authorized, no healthy deploy reverted. Resume with percollector execution/API wait or complete-cycle evidence; future separate version hour must verify all49first completions before timing.

Fresh loop17 drift review confirms AC3 explicit parked alternative is satisfied: remaining live-quiet criterion parked with both terminal failed receipts /Users/rob/repos/cf2otel/codex/live-loop17/hour-2.json and /Users/rob/repos/cf2otel/codex/live-loop17/hour-2-retry.json. This checks only parked-with-both-receipts, NOT sustained-hour success. Task remains Parked, AC1/2 open, no third retry/refund or source/rate change. Root prior withholding corrected; startup precondition miss retained in report.

Owner requested High-priority investigation of deployed polling performance. Fresh Grafana0.17.0 read around18:04UTC shows46/49completed collectors and zero scrape errors; email.routing/firewall.metrics/httpreq.metrics still never-success. Mean completed polls: HTTPthreats2373.98s, HTTPevents2198.50s, Logpushfailures2088.44s, firewall1720.37s, certificates1627.81s, DNS1512.22/1461.24s.1277APIrequests at shared0.5rps imply roughly42.6minutes request capacity. Sharedpacing strongly supported bottleneck, exact missingcollector queue/inflight state and starvation notproved. Most configured5mincadences therefore not sustained. Preserve frozenrate/burst until new authorized correction; nextloop should prioritize cause/reproduction, fairboundedpacing and realistic wholepoll budgets without weakening upstreamquota/window guarantees. Numeric safe receipt /Users/rob/repos/cf2otel/codex/live-loop17/poll-performance-1758.json.

Owner close out at18:10UTC. Final0.17.0startupreadback stopped before secondread, not an hour pass; healthy final deployment remains in place. High-priority performance investigation is next-loop priority. At18:04UTC46completed series, three never-success, no errors; durations19-40minutes prove configured5minute cadence not sustained. Source correction and exact in-flight/fairness attribution remain open, parked-live AC3alternative checked, no live-quiet acceptance.

Loop18 historical read at 18:20UTC resolves the three previously missing collectors: email.routing first poll43.14min, firewall.metrics48.84min, httpreq.metrics54.64min. Completion/success counters publish only after RunOnce returns; all later succeeded. Shared0.5rps pacing saturated continuously; exact live percollector request/queue breakdown unavailable. Independent design challenge rejects184 unbatched selections as irreducible quota demand: bounded envelope coalescing already authorized, request frequency distinct from node/query cost. Revised design in progress; no correction/deploy/live-hour pass yet.
<!-- SECTION:NOTES:END -->
