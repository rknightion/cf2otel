---
id: CFO-0085
title: aigateway.logs runs take 526s on average since the shared limiter and go stale
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-19 15:14'
updated_date: '2026-10-21 03:12'
labels:
  - bug
dependencies: []
priority: high
ordinal: 115000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured 2026-10-19 on the m7kni stack: aigateway.logs averaged 526 s per run over 24h against 41 s the week before last, with ~28,000 s/day of cf2otel_api_limiter_wait_seconds (about 7.8 h). Only 124 of ~288 expected runs completed, with no scrape errors, so ticks are skipped while a run is still going. Last success has been up to 3,694 s old, tripping cf2otel-collector-stale (900 s) about 16 times in a day. aigateway.coverage is unaffected (288 runs). The timing fits the shared-limiter work in CFO-0079. Do not raise the alert threshold: the alert is correct.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 aigateway.logs completes a run within its polling interval at the p90
- [ ] #2 cf2otel-collector-stale does not fire for aigateway.logs over a 24h window
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce insufficient REST throughput and combined-quota protection through the public client with fake upstreams. 2. Add a combined process bucket, allow REST to use spare capacity, and admit class/resource/total tokens atomically. Preserve configured overrides, pause handling, retries and redirects. 3. Verify configuration validation, quota and cancellation contracts, run remote just check, independent review and CodeRabbit before landing. 4. Publish the fix, verify exact-SHA CI/deployment, then retain live p90/24h-alert and declining-checkpoint-lag criteria open until actually observed.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Read-only baseline 2026-10-20: trailing 24h about 21 completed polls, mean 3912s, 84961 REST attempts, 21348 exported request rows, 41094s limiter wait and 41806s HTTP header duration. Current checkpoint age about 90666s and last-success age about 5650s. Existing histogram ends at 120s, so histogram_quantile p90 returning 120 is censored and cannot establish the 300s acceptance criterion. Body capture performs sequential detail/request/response GETs, 1.25rps process REST cap, max four catch-up windows of up to 15min. Investigating throughput and growing source lag before changing scheduling or quotas.

Candidate: combined process total 3.75rps/burst5, REST default 3.75rps sharing spare capacity, unchanged GraphQL class/scope caps; atomic resource/class/total admission meters retries and redirects. Per-log detail/request/response reads overlap with joined cancellation; serial parsing/emission and whole-second byte-budget commits retained. Public-client spare-capacity reproduction red (8/12 calls in 3s) then green; total-ceiling bypass mutation red. Registered scheduler with 200 full-capture rows per five minutes, 500ms fake upstream latency and ~1.1rps competing GraphQL commits in 228.33s versus 480.5s with former REST cap. Focused race tests green. Independent static review PASS. Added scrape-duration histogram boundaries through 7200s because old 120s tail censored p90. No live acceptance claimed. First remote gate failed before validation on macOS AppleDouble sidecars from archive xattrs; retry uses COPYFILE_DISABLE=1. An unrelated tracker-only commit arrived on main during work; preserved.

CodeRabbit completed with zero findings and all 20 changed paths reviewed. Remote retry lint found unused tokenBucket.acquire wrapper after admission refactor; removed it (trivial dead-code deletion). Final remote run now past fmt/lint/vet and running race suite. New cancellation/join regression passes with synctest. DoD2 conditional not applicable: no Dockerfile/goreleaser/image definition change; DoD3 no new signal/attribute names, only existing scrape histogram boundaries extended.

Original full gate timed out in TestPrometheusModesAndOccupiedBind after 10m; stack proves print-effective-config blocked writing stdout into undrained captureStdout pipe. A bounded 150kB-output regression fails on that helper and passes after concurrent draining. Focused local and remote CLI race tests pass; delta CodeRabbit complete with zero findings. Corrected full gate running from isolated worktree so unfinished token-usage work is excluded.

Final isolated remote just check PASS: run 20261021T012144Z-cf2otel-71533f9c, candidate tree 7be1c7c8cad5e434531d456069ff3e83a62058f1 on c7e42eb. All race suites, lint, vet, tidy, build, vuln and generated checks passed. CodeRabbit complete zero findings for primary 20-path candidate and stdout-capture delta; independent review PASS. Both live acceptance criteria remain open. Publishing source correction; resume after exact-SHA CI and final rollout epoch, then observe p90/24h stale plus checkpoint lag.

Limiter source published as 1661d4b. Exact-SHA CI exposed existing module Go1.27.2 vulnerability GO-2026-6809; next source commit aligns Go1.27.3 with the existing Docker pin and passed final composed remote gate 20261021T012928Z-cf2otel-2f357034. Live commit has not yet been observed. Bounded 36h read-only observer prepared at /Users/rob/repos/cf2otel/codex/cfo85-observe.py; receipt /Users/rob/repos/cf2otel/codex/cfo85-observation.json. It waits for final exact-SHA CI/Release/auto-rc, fences build/process identity, requires fresh checkpoint and last success, watches actual stale rule, and measures a continuous 24h <=300s histogram ratio. Both live AC remain open until a real pass and readback.

Live owner-requested check 2026-10-21 01:50 UTC: exact final efb6067 build observed and GitHub CI39528251783 completed success. Process uptime about588s at sample, aigateway.logs929physical requests,562.08s aggregate limiter wait and387.21s aggregate HTTP duration, zero completed-poll/checkpoint/exported-row series yet and scrape errors initialized0. Existing stale rule Alerting. This is first-run catch-up, NOT demonstrated improvement or recovery; both AC remain open. Receipt /Users/rob/repos/cf2otel/codex/cfo85-live-check-20261021.json. Missing checkpoint telemetry during restored first run motivates generic alert work and startup cursor observation.

Follow-up live sample around01:56 UTC: efb6067 process completed2 failed aigateway.logs polls, total838.460s (mean419.23s), no success/checkpoint series, scrape error class other=2 and commit-failure counter0. Source recovery NOT established. Same-source auto-RC0.18.1-rc.12 began a new epoch, so never splice its counters into the earlier process. Loki query for runtime collector-failure lines returned none; precise failure cause remains unobserved. Existing stale alert remains correct.

After bundling generic poll/backlog alerts, observer retargeted to23fbd865ae06b3673dac5547143be50cfd51253f. Current bounded observer /Users/rob/repos/cf2otel/codex/cfo87-observe.py, receipt /Users/rob/repos/cf2otel/codex/cfo87-recovery-observation.json, process identity /Users/rob/repos/cf2otel/codex/cfo87-recovery-process.json. Prior receipts retained. Requires final CI/Release/auto-rc success, actual two new alert rules, exact process identity and24h fresh source checkpoints/success; Grafana-sync overall failure is separately reported because only existing extra-rule ownership failed verification. Both recovery AC remain open.

Read-only diagnosis 2026-10-21 ~03:00 UTC, epoch fenced on commit 23fbd86 + process start 1792548903 + one instance (service_version 0.19.0-rc.1). One successful aigateway.logs poll took 2100.9s and advanced the checkpoint only 654s of source (0.31x wall time; 4 commits, catch-up windows 3), so checkpoint progress does NOT exceed wall time. Cause: source density, not failures. The cursor (~2026-10-20 00:05 UTC) is inside the jg fan-out spike: exported rows were 10623/h and 9096/h for 2026-10-19 22:00-24:00, and REST paging shows 10000-15000 logs in 2026-10-20 00:00-01:00, then under 1000/h for every sampled later hour. Capture ceiling: the process made 8215 Cloudflare calls in ~2100s (about 3.9 rps against the 3.75 combined ceiling); GraphQL collectors took about 38%, leaving aigateway.logs about 2.4 rps. It exported 1313 logs for 5072 REST calls (3.86 calls per log; floor 3.02), about 1950 logs/h. The excess is replay: each budget-truncated window re-lists every page to the 15-minute window end before committing about 16 MB. Ruled out this epoch: upstream failures (scrape errors 0, retries 0), export pressure (commit failures 0, no export-error series). The success-counter-1/last-success-0 reading is selfobs snapshot skew, not lost state: Poll() exports the success counter immediately but last-success, elapsed and checkpoint-age gauges refresh only in the 5-minute selfobs snapshot; the 02:55:53 snapshot showed last success 02:51:04 and elapsed 2100.9s. Live Ratelimit-Policy on the runtime token is default q=1200 w=300 (4 rps), so the ceiling has little headroom on one token. Detail GET already carries request_head/response_head bodies with *_head_complete flags; 20 of 20 sampled logs had both complete and the request head matched the raw /request body byte-for-byte (117379 bytes), so the two body GETs per log are redundant. Fix tracked separately. Earlier epochs' poll-failure cause stays unobserved: process logs are not in Loki.
<!-- SECTION:NOTES:END -->
