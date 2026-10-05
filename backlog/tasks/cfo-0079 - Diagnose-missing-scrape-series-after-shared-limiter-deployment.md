---
id: CFO-0079
title: Diagnose missing scrape series after shared-limiter deployment
status: Parked
assignee:
  - '@loop22-root'
created_date: '2026-10-03 15:23'
updated_date: '2026-10-05 12:20'
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

Loop19: resume preserved packing/fair-limiter draft; owner-authorized exported-client red/green replaces registered scheduler witness. Pre-land security review and CodeRabbit; just check plus repeated race gate; deploy via green main push, confirm build SHA and every first poll, unchanged 65-minute live-hour and final interval ages.

Loop20: owner grants two additional clock-only attempts. Refuse ConfigureProcessRateLimitWithClock outside testing.Testing; preserve packing/fairness semantics. Rebase onto current main, unchanged public-client and FIFO witnesses, delta security and CodeRabbit, candidate gates, landing and exact-SHA CI, then all49 first polls followed by unchanged65minute proof and interval ages.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop17 D79 read-only mapping attributes hour-witness failure to incomplete completed-poll coverage, not demonstrated error or starvation. Poll counters publish only after RunOnce returns. At15:29:32Z46completion series, all49error serieszero, three names email.routing/firewall.metrics/httpreq.metrics have lastsuccesszero;1064APIrequests compatible with shared0.5rps pacing but no percollector queue/inflight proof. Both HOUR2 and its only retry failed after2reads due count39/49 then46/49, all present attempt/success counts match. Root started before direct proof of all49first cycles, an avoidable precondition miss, not new source evidence. No rate/burst/config/source change or budget reset authorized, no healthy deploy reverted. Resume with percollector execution/API wait or complete-cycle evidence; future separate version hour must verify all49first completions before timing.

Fresh loop17 drift review confirms AC3 explicit parked alternative is satisfied: remaining live-quiet criterion parked with both terminal failed receipts /Users/rob/repos/cf2otel/codex/live-loop17/hour-2.json and /Users/rob/repos/cf2otel/codex/live-loop17/hour-2-retry.json. This checks only parked-with-both-receipts, NOT sustained-hour success. Task remains Parked, AC1/2 open, no third retry/refund or source/rate change. Root prior withholding corrected; startup precondition miss retained in report.

Owner requested High-priority investigation of deployed polling performance. Fresh Grafana0.17.0 read around18:04UTC shows46/49completed collectors and zero scrape errors; email.routing/firewall.metrics/httpreq.metrics still never-success. Mean completed polls: HTTPthreats2373.98s, HTTPevents2198.50s, Logpushfailures2088.44s, firewall1720.37s, certificates1627.81s, DNS1512.22/1461.24s.1277APIrequests at shared0.5rps imply roughly42.6minutes request capacity. Sharedpacing strongly supported bottleneck, exact missingcollector queue/inflight state and starvation notproved. Most configured5mincadences therefore not sustained. Preserve frozenrate/burst until new authorized correction; nextloop should prioritize cause/reproduction, fairboundedpacing and realistic wholepoll budgets without weakening upstreamquota/window guarantees. Numeric safe receipt /Users/rob/repos/cf2otel/codex/live-loop17/poll-performance-1758.json.

Owner close out at18:10UTC. Final0.17.0startupreadback stopped before secondread, not an hour pass; healthy final deployment remains in place. High-priority performance investigation is next-loop priority. At18:04UTC46completed series, three never-success, no errors; durations19-40minutes prove configured5minute cadence not sustained. Source correction and exact in-flight/fairness attribution remain open, parked-live AC3alternative checked, no live-quiet acceptance.

Loop18 historical read at 18:20UTC resolves the three previously missing collectors: email.routing first poll43.14min, firewall.metrics48.84min, httpreq.metrics54.64min. Completion/success counters publish only after RunOnce returns; all later succeeded. Shared0.5rps pacing saturated continuously; exact live percollector request/queue breakdown unavailable. Independent design challenge rejects184 unbatched selections as irreducible quota demand: bounded envelope coalescing already authorized, request frequency distinct from node/query cost. Revised design in progress; no correction/deploy/live-hour pass yet.

Loop18: implementation attempts2; parked authority before unproved code landing. Draft in persistent polling worktree implements packing/fair dual quotas, safe validation and testclock migration; config/cfapi and migrated domain checks pass but allsix realregistered synctest cases deadlock because existing namecache mutex holds across paced REST while siblings wait. This is NOT authentic pacing red. Root inspected manualclock alternative: owned scheduler/admission injection cannot control unowned domain cacheTTL/currentmonth/snapshot/retention clocks, so full frozen clock semantics would be silently lost. Root rescue inspected only, no third change-and-verify attempt charged. No fullgate/race/CodeRabbit/security/baseline-red/candidate-green or F1 commit/push/release/deploy/hour proof. Preserve draft; resume after narrow domainclock ownership or independently validated witness amendment is authorized. AC2 stays open; existing AC3 checked only parked alternative from loop17, not real pass. Three initially missing collectors now precisely attributed as delayed firstpoll completions; percollector live request attribution remains unavailable.

Loop19: total implementation attempts4 (two historical, two resumed). Exported-client identical semantic ledger baseline552 physical requests versus candidate69 passes packing reproduction; REST FIFO/cancellation passes. Pre-land security rejects production-exported caller clock: valid capacities can be accelerated without wall-time pacing, contrary to private-only seam contract. No landing/deploy/live-hour. Final gate initially blocked by lint-lock infrastructure; unchanged-state gate retry in flight. Ceiling exhausted; resume needs owner-raised attempt ceiling to remove public clock bypass, then review/gates/deploy and live proof. AC2 remains unchecked; AC3 remains only prior parked alternative.

Loop19 final unchanged-state gate retry passed just check and go test -race -count=10 ./internal/cfapi/ ./internal/config/, exit0. Tested dirty worktree on base f1a82ed280a1dddc856e8fdca722ac204592a8cb plus candidate patch b57b8595258db4982f2fa255e151d840924c7915359f35c94546f55ed744e248. Security clock-bypass blocker remains; no acceptance, commit, push, deployed proof or composed gate. Four attempts exhausted. Gate green is not landing permission.

Loop20 adopted. Historical attempts4 remain; two owner-granted clock-only additional attempts. Current main already publishes the newer loop19 tracker records. Root owns tracker writes; no pushes during live proof.

Loop20 clock-only additional attempt1 passes at a756b7af7e6524143137731a5ceaaf1d0d2a9c52 on base0f69a0b60321b4aa5ea3233479afe67d6c30eba7. Frozen public-client ledger552 physical requests to69, identical semantic hash; FIFO A,C,newcomers; production normal executable clock rejection red/green. just check and race count10 exit0 after lint-lock infrastructure retry. Independent security accepts clock-only delta; CodeRabbit complete with two owner-rejected unchanged-path majors, no new delta blocker. No push yet; live acceptance and composed gate/CI outstanding.

Loop20: total production implementation attempts5 (historical4 plus owner-granted clock-only attempt1; second additional clock-only attempt unused). Candidate production a756b7af7e6524143137731a5ceaaf1d0d2a9c52 landed through0519970c1257738eb4f4f947a6db33b8161cd8cf; gates, security, CodeRabbit disposition and exact-SHA CI green. Deployed RC0.17.1-rc.6 fails live performance envelope: first email.routing2337.420s, firewall.metrics1748.335s, htt preq.threats2337.419s;48/49 completed after~100minutes, htt preq.metrics no completed attempt; several5minute collector success ages exceed twice interval despite zero error counters. No65minute hour started. Owner correctly challenged longpolls as unresolved issue; root prior incomplete-proof framing corrected. Goal-authorized singlecommit production rollback in progress, no code retry afterdeploy. Private receipts first.json and first-retry.json retained; original first receipt not-observed due repaired1ms helper skew, not exporter failure. AC1/2 remain open; AC3 remains only loop17 parked alternative, not real sustainedhour pass. Resume needs a separately authorised realcollector throughput/catchup diagnosis, then correction budget and new deployed proof.

Loop21 live measurement interrupted by verified unhealthy instrumentation epoch:14collectors23scrapeerrors including8repeated failures versus0baseline. Goal requires rollback inonecommit; no throughput fix. Sanitized partial cumulative request/wait/duration table /Users/rob/repos/cf2otel/codex/live-loop21/attribution.json; no all49firstpollor90minute completed, no exact epoch increase/time-share or per5minute demand claimed. AC1 remains unchecked; cause remains unattributed. Resume recommendation: separately budget authentic runtime-error reproduction and restore measurement before throughput correction.

Correction: originalversion46cbb26 has22errors(not23), newsameSHA0.18.0-rc.1 has0errors/21successes after autoRC37243958078. Singleepoch broken by automaticRCredeploy; codecausation NOT proven. Partial historicalinstant table atalarm:49rows,441cumulative requests, wait/duration sums/counts; no reliable zero baseline, exactfirstpoll shares or5minute rate. Conservative explicit goal rollback chosen; AC1 unchecked. Re-grade release/epoch fence before renewed measurement.

Partial table reduction (not AC1):49collectorrows,441cumulative original-version requests,20688.666103seconds aggregate limiterwait and109.917351seconds API duration across concurrent requestseries. Leaders:requests email.sending33,healthchecks.events29,dns.events/firewall.events28; wait dns.events842.411s,dns.metrics840.896s,httpreq.metrics840.821s; API time tunnels.status10.873s,email.sending6.015s,healthchecks.events5.629s. These totals are neither firstpolltimefractions nor exactfive-minute demand; counterzero/epoch/source fence absent. Cause of22errors across14collectors remains unattributed, sameSHA RCnewversion0errors. Proposed nextscope: freeze/identify finalRCredeploy epoch, capture authenticated sanitized error cause and startupzero metrics, then measure all49or90minutes. Recommend atmost2implementation attempts for authentic registered/runtime error and epoch prerequisite only; no throughput correction until attribution acceptance.

Loop22 L1 observation notstarted, AC1unchecked: final3e85282a exactCI37305970420success and autoRC37307606314fullcompletion12:15:56UTC; RC0.18.0-rc.6scrapes visible12:15:30(26searlier). Identity-awarehistory through12:19 found87series/0decreases, no qualifyingpostcompletionreset. Actualmetricnames/labelkeys discovered, no guessedzeros or measurement accepted. Receipt /Users/rob/repos/cf2otel/codex/live-loop22/L1-final-history-1791202745.json. Implementationattempts parent historical5unchanged; currentchildrenR1total3,M2conservative2; liveprobe consumesnone. Parkowner: authorise sameSHArestartafterfullRCcompletion or amend fence to demonstrablysettledpublication-linked epoch; rootcannotwaive it, SSH/restartnotgranted. No ongoingregression demonstrated/no rollback. Workflowdispatch follows parkedL1, noactiveobservation.
<!-- SECTION:NOTES:END -->
