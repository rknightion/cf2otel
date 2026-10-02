---
id: CFO-0071
title: Zero-initialise scrape error counters for enabled collectors
status: In Progress
assignee:
  - '@loop16-root'
created_date: '2026-10-02 13:52'
updated_date: '2026-10-02 14:58'
labels: []
dependencies: []
priority: high
type: bug
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A never-failing collector has no error series, so error-counter coverage cannot distinguish a healthy collector from one that is not reporting. Loop 16 makes zero points visible without changing the attempts-equals-successes delivery witness.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Through the real SDK and an OTLP HTTP collaborator, two enabled collectors that do not fail each export a zero-valued scrape error point; the test fails on the base by assertion.
- [x] #2 A later real failure adds one with its real error class, window-committing collectors initialise the commit-failure counter at zero, disabled collectors export neither point, and dry-run remains side-effect free.
- [ ] #3 The released running exporter exposes scrape error series for every enabled collector at the next sustained-hour read.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
ZERO initialises existing error and window-commit counters on enabled registration, proves zero points and subsequent classified failures through real SDK export, then runs the lane gate and CodeRabbit before exact-SHA review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop16 ZERO-I1 consumed one implementation attempt, zero infra retries. Real-SDK zero-point witness and CodeRabbit passed, required gate failed because existing error-class test expected one point and now sees the intended registered selfobs zero point. ZERO-I2 is authorised to update the point-set assertion without weakening fixture class, count or bounded attributes, then rerun gate and review. No candidate committed or live claim.

Loop16 ZERO source accepted on REV-ZERO PASS 43d9c70305c7706a971d326a6e60ef81177fa9a3. Independent reviewer copied only new test onto literal unchanged base, compiled and failed missing-zero assertion; exact candidate passed real-SDK OTLP and dry-run boundaries. Intended error-class point-set update retained all fixture assertions. Two implementation attempts consumed, review-repair zero, infrastructure zero. Exact patch cherry-picked as ca6e69e and pushed in 4f14d92189703ab26752f663d5c6b3502e0e2da4, integrated gate passed. AC3 remains live-only parked behind failed deploy-kit acceptance; running release is unchanged. Landing CI pending watcher.
<!-- SECTION:NOTES:END -->
