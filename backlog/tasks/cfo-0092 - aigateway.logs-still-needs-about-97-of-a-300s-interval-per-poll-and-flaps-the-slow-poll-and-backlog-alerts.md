---
id: CFO-0092
title: >-
  aigateway.logs still needs about 97% of a 300s interval per poll and flaps the
  slow-poll and backlog alerts
status: In Progress
assignee:
  - '@codex'
created_date: '2026-11-05T21:58:00Z'
updated_date: '2026-11-06T12:12:00Z'
labels:
  - bug
dependencies: []
priority: high
ordinal: 122000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured 2026-11-05 on the m7kni stack, build e7c8ae0 (0.20.1-rc.5): aigateway.logs poll elapsed sat at 0.95-0.97 of its 300s interval for most of the evening, so any rise in AI Gateway traffic pushed it over. API calls for the collector rose from about 100 to 2,200 per 15 min between 16:00 and 21:15 UTC while per-call latency stayed flat at about 0.85s and limiter wait at 0.5-1s. Elapsed/interval peaked at 3.2 and backlog/interval at 3.76 around 21:15-21:20, firing cf2otel-collector-slow (12:00, 21:02) and cf2otel-checkpoint-backlog (21:17); it has flapped like this most days since 2026-10-29. Over 6h aigateway.logs made 18,600 of the process's Cloudflare calls, about 0.86 rps and 60% of the total. Stopgap applied on the compose host the same evening: aigateway.logs interval 10m in the host config (previous file kept in the host's cf2otel backups folder). CFO-0085 (runs too slow under the shared limiter) and CFO-0089 (one detail read per log, stop listing at the commit budget) are both In Progress with their live criteria never observed; check first whether the calls-per-log target from CFO-0089 holds live, then decide whether 10m stays or the collector needs more throughput. Do not raise the alert thresholds.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Live calls per exported aigateway.logs record are measured in one fenced epoch and compared with the CFO-0089 target of at most 1.2
- [x] #2 A decision on the interval is recorded: keep 10m, return to 5m after a throughput fix, or another value, with the measurement it rests on
- [ ] #3 cf2otel-collector-slow and cf2otel-checkpoint-backlog do not fire for aigateway.logs over a 24h window at the chosen interval
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Measure current fenced calls per record and poll health; reproduce sequential detail latency with bounded fake upstreams; implement ordered bounded concurrency if supported; run focused regression checks, required gate and reviews, then observe live throughput. Preserve 10m interval pending evidence; leave 24h criteria open until observed.

Owner authorized deploy verification: wait exact candidate CI/image publication, pull and run on compose host, inspect runtime errors, merge release PR after clean runtime check, then inspect every15min with build/process epoch fencing until measured impact is established. Keep24h alert criterion open until full observed window. Do not push tracker changes during live proof.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live diagnosis: sampled metadata-only records explicitly return collect_log_payload=false with null complete heads; both raw body endpoints return 404/code7002. Current collector still performs both reads, costing three requests per such log. Official logging docs confirm false disables payload storage while retaining metadata. Prioritize skipping these impossible reads, preserving detail metadata and unavailable-body counters. Missing/null flags retain fallback. Sparse cross-second prefetch limitation also reproduced but is deliberately constrained by budget semantics; defer until call amplification is fixed.

Candidate skips raw request/response GETs only for explicit collect_log_payload=false; detail value overrides list, unknown retains fallback, complete heads remain usable. Real-HTTP regression failed with two unnecessary reads then passed. Keep 10m interval pending deployed evidence; no alert threshold changes. Advisory review found no introduced duplication and fetchLog complexity 14 to17, justified by tri-state precedence and per-side fallback. Remote gate hosts unreachable; local isolated just check running.

Focused race regressions passed; dsreview PASS with zero findings. Full local gate passed lint/vet and is running race suite. Sparse concurrency reproduction retained as follow-up evidence; no concurrency/budget contract changes in this candidate.

Final isolated local just check PASS, including all race suites, build, vulnerability and generated Grafana checks. Remote hosts were unreachable. dsreview PASS with no findings; advisory review assessed. Source fix ready to publish; live calls-per-record and 24h alert acceptance remain open. No new signals or packaging change.

Rollout checkpoint: exact b05d69e CI42128369086 and Release42128369543 passed. Compose host pulled and ran exact image; already automatically updated, healthy with0 restarts and0 runtime errors. First completed diagnostic poll:28 source spans,40 physical requests,95.835s total,69.444s collect,26.389s commit. Prior0135e04 epoch baseline:3 successes,488 source records,514 requests,510.402s total. Release PR106 passed own CI42128447280, reviewed metadata-only diff including both fixes, merged as153f5313811341526adaa52ceb718362c653d066. Await final release/CI and process stabilization, then run15min inspections with stable epoch; no pushes during proof. Baseline/local read helper and bounded observer are in machine-local codex/. 24h alert criterion remains open.

Current observation state: release153f5313811341526adaa52ceb718362c653d066 / version0.20.1 pulled and running healthy, process start1793921557.2273192. Release42129579918, CI42129579022 and final auto-rc42130956461 all success; prior RC also completed. Runtime errors0. Impact watcher PID2316868/session94254 writes /Users/rob/repos/cf2otel/codex/cfo92-release-impact-live.json every900s (bounded2h); unchanged live-hour helper session70164 writes /Users/rob/repos/cf2otel/codex/cfo92-release-hour.json (14 reads/300s,49 collectors, actual intervals). Both sleep-protected with Linux systemd-inhibit because caffeinate is unavailable. First impact read:64 spans,75 requests,165.543s,0 failed polls. One failed monitor bootstrap receipt retained separately, not proof. Next action: inspect15min receipts, verify epoch unchanged and final65min delivery witness; no pushes during observation.

Observation correction: short-name SSH began failing because it resolves to the LAN SSH service and the forwarded agent has no identities. Explicit Tailscale SSH to the existing host succeeds; manual host check confirms original process uptime, healthy,0 restarts and no ERROR/FATAL logs. Original failed watcher receipt retained. Resumable continuation session47580 uses explicit Tailscale SSH and writes /Users/rob/repos/cf2otel/codex/cfo92-release-telemetry-impact.json, preserving the initial observation plus a61s-delayed second read. Original unchanged hour witness still running. Second telemetry read:2 successful polls,103 records,118 requests(1.146/record),123.977s mean,0 failed scrapes. Do not treat the host-check gap as uninterrupted host evidence.

45-minute observation: same release/process,5 successful AI polls,0 failed polls,146 records,189 requests(1.295 overall),71.532s mean. Poll durations165.543,82.411,73.533,25.423,10.752s; lower traffic confounds speedup claims. First2 polls met1.2 target; later missing-body fallback4xx raised aggregate. Tempo operational traces verified correlated poll/API spans:27 total4xx errors across successful polls, consistent with tolerated404/7002 body-unavailable handling. Container remains healthy/no restarts/no runtime ERROR or FATAL; latest checkpoint backlog290.335s, down from590.335s. All3 AI slow/backlog/stale alerts currentlyNormal with healthy evaluations. Keep10m interval; no threshold changes. Delivery hour witness still running/pass. Host short-name DNS fixed persistently with DHCP domains routing-only; explicit Tailscale route remains in observer. Next: final15min sample and unchanged65min delivery completion. No push during observation.

Observation complete: unchanged sustained delivery witness PASS over3900s,14 reads,49 collectors,session70164 exit0. Receipt /Users/rob/repos/cf2otel/codex/cfo92-release-hour.json. Impact observer exit0/evidence_collected over3600s:6 completed successful AI polls,278 committed records,352 physical calls(1.266 per record),mean105.382s,max274.630s,zero failed polls; aggregate excludes in-flight next-poll counters. First2 polls met<=1.2; payload fallback and boundary work raised the overall ratio. All observed completed polls stayed below600s; traffic variation prevents a clean causal throughput claim. Keep10m based on busiest poll274.630s and174.034s commit cost; do not restore5m on this evidence. Final short-name SSH host check succeeded after DNS fix: release0.20.1/153f5313811341526adaa52ceb718362c653d066,original start2026-11-05T23:32:37Z,healthy,0 restarts,0 ERROR/FATAL lines across process lifetime. Operational traces and poll summaries verified live. CFO-0095 (explain and reduce AI commit/export time) captures the measured remaining cost. AC3 stays open:24h quiet-alert window has not been observed. No observation processes remain active and no unattended24h monitor is implied. Board-only commits remain local for the next ordinary push.

2026-11-06livehistory verification: actual slowAlerting Nov5 21:01-21:12 andbacklogAlerting21:16-21:21,22:36-22:46 remainin trailing24h;AC3 cannotpass. All3rules currentlyNormal/healthok. Processmonitor launched as systemd cf2otel-quiet-proof-20261106.service,PythonPID3738438,every900s,deadlineNov7 14:11:26UTC. Receipt /Users/rob/repos/cf2otel/codex/cfo92-quiet-monitor.json;script /Users/rob/repos/cf2otel/codex/cfo92-quiet-monitor.py. Initialread successful,1441/1441interval/freshnesssamples;withholdsPASSdueprior300sinterval/firing/MissingSeries. Requires full24h600sinterval,freshcompletecoverage,knowninitialstatefromprecedinghistory,noalert/error/missingseries,andhealthyNormalrules. Earliest scheduledpassaboutNov6 22:56UTC ifclear. Authenticationerror/historytruncation cannotpass. No claimofcompletion;nextaction inspectterminalreceipt andfinalizeonlyifPASS.
<!-- SECTION:NOTES:END -->
