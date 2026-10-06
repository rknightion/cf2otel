---
id: CFO-0083
title: Export process start time as a self-metric
status: Parked
assignee:
  - '@lane-worker-push'
created_date: '2026-10-06 16:37'
updated_date: '2026-10-06 17:38'
labels:
  - telemetry
dependencies: []
priority: medium
type: enhancement
ordinal: 112000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
In-place auto-RC restart retains stale old-process series sharing instance identity with a different version; explicit process start enables unambiguous lifetime-fenced proof. Owner authorized this follow-up after CFO-0082 (Upgrade OpenTelemetry Go to the v1.47 line) manual acceptance.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 cf2otel.process.start_time has unit s and description Unix time the cf2otel process started.; declared in semconv selfobs and metadata, captured once and emitted beside build.info.
- [x] #2 Update /Users/rob/repos/cf2otel/docs/signals.md and required generated files; introduce no other signals or attributes.
- [x] #3 Existing telemetry receiver regression observes the real export and fails on base for the missing signal, passes candidate; no tests weakened.
- [x] #4 Update /Users/rob/repos/cf2otel/LOOP.md auto-RC trap: full label epoch build_commit plus process-start plus exactly one instance; file remains <6KB.
- [x] #5 just check and CodeRabbit complete with critical/major findings resolved; exact-SHA ci-success green.
- [ ] #6 Root post-auto-RC GET-only proof before Done matches accepted SHA and exactly one process-start series later than run start and not future.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an OTLP receiver regression proving a real exported process-start gauge is present, correctly described and unchanged across collections and Stats construction; record missing-signal failure on base.
2. Capture process start once at package initialization, declare the sole new metric and emit alongside build.info; update signal catalogue and auto-RC epoch trap.
3. Run focused regression, just check and guarded CodeRabbit review; return uncommitted/unpushed candidate for root security review, leaving live acceptance and Done to root.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Receiver regression failed against unchanged base selfobs implementation for missing exported process-start metric, then passed with package-initialized capture. First just check failed with TestPollAndCollect reporting gauges=5: implementation/test expectation mismatch after intentional addition of a fifth gauge. Updated that test count and strengthened assertion of the new metric, captured value and absence of attributes; no existing gauge assertions removed. Process timestamp uses fractional Unix seconds, captured once at package initialization. LOOP size is 5783 bytes. No live calls performed; root security review, authorized landing/CI and post-auto-RC proof remain pending.

Final candidate just check passed (exit 0), including generated manifest checks: no generated-file updates needed. CodeRabbit completed with findings=0 and reviewed all eight changed files. Evidence: /tmp/l30-PS-evidence/regression-base.log (missing exported metric), /tmp/l30-PS-evidence/regression-candidate.log (passed), /tmp/l30-PS-evidence/gate.log, /tmp/l30-PS-evidence/coderabbit.log, /tmp/l30-PS-evidence/candidate.diff. Uncommitted/unpushed review-ready candidate in /Users/rob/repos/cf2otel-wt/l30-PS; task stays In Progress. Root security review, explicit authorization to land, exact-SHA ci-success and live post-auto-RC proof pending.

loop30, implementation attempts 1, parked on frozen live-check data failure. Landed eeb706ddbabcd06dee897cc3a2b4d2e63f0c6abf; exact-SHA CI 37502384658 and auto-RC 37503788751 successful; independent security review and CodeRabbit complete, composed just check green. At query epoch 1791308264.088 one GET showed 2 build series, 2 process-start series (1791308108.2823317 and 1791307506.7767372), one distinct instance; older start is below exclusive auto-RC start bound1791307697. Build label evaluator did not capture commit/version, so expected SHA unverified. Collector age read had 98 series with max1791308264.088 seconds; stale/zero-valued epochs are not proof of live collector health. Initial credential read failure made zero network requests; sole authorized retry made one successful GET. No data retry, no release merge. Resume requires owner authorization for a new read after stale lookback, exact existing-label introspection and current main CI; do not rerun P1 or repair helper. Foreign Renovate Docker digest commit b121fdbd264825d945b047afdf7fa3eb86b07924 arrived during rollout and was preserved.
<!-- SECTION:NOTES:END -->
