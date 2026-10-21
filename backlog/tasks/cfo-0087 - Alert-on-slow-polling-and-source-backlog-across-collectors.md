---
id: CFO-0087
title: Alert on slow polling and source backlog across collectors
status: Done
assignee:
  - '@codex'
created_date: '2026-10-21 01:48'
updated_date: '2026-10-21 01:59'
labels: []
dependencies: []
priority: high
type: enhancement
ordinal: 117000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Fresh scrape successes can hide an ever-growing source backlog, and completed-duration histograms cannot identify a currently stuck poll. Bundle generic collector-level alerts using actual runtime intervals and source holdbacks, preserving replica identity and avoiding false backlog on intentional dataset lag or snapshots.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Slow-poll alert covers active and completed polls exceeding their configured interval
- [x] #2 Backlog alert covers window collectors exceeding three intervals beyond their actual source holdback, including restored checkpoints before first successful poll
- [x] #3 Generated alert fixtures cover healthy, slow, backlog, hourly, snapshot absence and mixed replicas; operational docs explain thresholds and limitations
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Add runtime interval, active-or-last elapsed and holdback-adjusted backlog gauges; wire scheduler start/completion and seed checkpoint observations. Parallel alert generator lane uses frozen signal contracts. Prove runtime and PromQL regressions, independent review and CodeRabbit, remote gate, then publish and verify CI and alert sync.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Added interval, elapsed(max active vs last completed), and source-holdback-adjusted backlog gauges; actual dynamic LagAt used, restored/initial checkpoints observed before fetch. Registered scheduler test green and authentic mutation red when startup observation removed; repeated modest-overrun regression green/mutation red. Promtool 10-case fixtures red then green, Python alert fixture driver includes new file; independent runtime review PASS. Source alert thresholds: slow >1 interval, backlog >3 intervals, each pending5min, per-replica normalization before worst collector reduction. Full remote gates and CodeRabbit running.

Full gate found startup cursor observation had reused commit-only OnCheckpoint callback, breaking catch-up cancellation and adaptive delivery witnesses. Added separate OnWindowStart observer and retained OnCheckpoint timing; focused cancellation now passes and independent final runtime review PASS. Earlier adaptive focused rerun was bounded at90s but that existing production-budget test needs about189s; timeout is not a pass, final full remote gate retains its normal bound. CodeRabbit minor descriptions/runbook-placement findings corrected. Final gate/review still running.

Final remote just check PASS: run20261021T015347Z-cf2otel-f72dc361, tree6cb532ea7fd8ad19f68383e398e82d022d68563f on efb6067. All race tests (including unchanged adaptive delivery/cancellation), lint/vet/tidy/build/vuln/generated checks passed. Ten new PromQL fixture cases plus existing fixture suites pass. Independent review PASS; CodeRabbit completed all15 candidate paths with2minor docs findings, both fixed. No packaging definition changes; conditional just ci not applicable. Source package complete; live publication/readback tracked separately in session.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bundled collector-slow (>one interval) and checkpoint-backlog (>three intervals excluding dynamic source holdback) alerts, pending5min, with actual registered cadence and per-replica normalization. Added active/retained duration, interval and adjusted-backlog gauges; restored cursor telemetry before first fetch uses a separate callback preserving commit-only checkpoint semantics. Full remote gate, independent review and CodeRabbit passed; fixtures and runbook cover hourly/snapshot/mixed-replica behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
