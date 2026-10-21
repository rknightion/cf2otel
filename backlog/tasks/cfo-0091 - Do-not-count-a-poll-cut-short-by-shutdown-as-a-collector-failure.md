---
id: CFO-0091
title: Do not count a poll cut short by shutdown as a collector failure
status: Done
assignee:
  - '@claude'
created_date: '2026-10-21 05:47'
updated_date: '2026-10-21 05:58'
labels:
  - bug
dependencies: []
priority: medium
ordinal: 121000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every redeploy recorded aigateway.logs scrape errors (class other): the in-flight poll is cancelled at SIGTERM and reported through OnPoll as a failure, and because a long poll usually overruns its ticker, runEntry's select can pick the queued tick over ctx.Done and start a second poll that fails at once with context canceled. Seen in Loki at 2026-10-21 04:25:02 UTC as two collector run failed lines 35 microseconds apart. This inflated error counters and the poll alerts on every deploy and misled CFO-0085 diagnosis.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A poll that returns after the scheduler context is cancelled is logged at INFO as interrupted by shutdown and is not reported through OnPoll
- [x] #2 After cancellation runEntry starts no further poll, even when a tick is already queued
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Red first: TestShutdownInterruptsPollWithoutFailureOrRetry reported 2 polls for one shutdown with a tick queued (the live 04:25:02 pattern). runEntry checks ctx before each poll and returns, logging INFO, when RunOnce fails with context.Canceled after shutdown began; CodeRabbit's one minor finding (suppress only cancellation errors, not any error racing shutdown) applied. Collector and cmd race tests pass; remote just check exit 0 before the errors.Is tightening, which re-passed the collector race suite. DoD2/DoD3 not applicable.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shutdown no longer records collector failures: the scheduler stops before starting a poll once its context is cancelled, and a poll that returns context.Canceled after shutdown began is logged at INFO instead of reported. Removes the two spurious aigateway.logs scrape errors every redeploy produced. The process being replaced by this deploy still runs the old code, so its shutdown records them one last time.
<!-- SECTION:FINAL_SUMMARY:END -->
