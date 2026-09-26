---
id: CFO-0035
title: Restore complete release notes for merged email collectors
status: Done
assignee: []
created_date: '2026-09-25 14:51'
updated_date: '2026-09-26 11:42'
labels: []
dependencies: []
references:
  - 'https://github.com/rknightion/cf2otel/pull/20'
priority: high
type: bug
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop 6 landed the email feature through a reconciled local branch at main 7a7bbdd, but release-please PR #20 at that exact base proposes 0.5.2 and lists only three fixes. Its generated CHANGELOG omits the Email Routing and Sending feature commit a4aeb08 and other conventional commits reachable since v0.5.1. The loop release gate requires the feature and every newly landed fix in the release notes, so CFO-0024 deployment is parked without merging this PR. Determine why the merged history is omitted and correct the release candidate without rewriting published history.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The release candidate changelog at a named main SHA lists the Email Routing and Sending feature and each newly delivered fix since v0.5.1, without duplicating patch-equivalent fixes already in v0.5.1.
- [x] #2 The candidate version reflects the feature under the repository release policy, and every required workflow at its base SHA concludes successfully before merge.
- [x] #3 A reproducible check demonstrates that a future merged local branch with conventional commits is represented in the generated release notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7: release notes checker landed at 50e1561 and alias repair at 198d4dc. PR #20 at base 1daf4be and head a4a8cff proposed 0.6.0, listed the Email Routing/Sending feature and all 15 required conventional subjects; just relnotes-check passed. CI 36237411272 had ci-success success; required latest base workflows passed. PR #20 merged as eb5c2c7; Release 36238560403 succeeded and published v0.6.0. The earlier superseded auto-rc run 36237432752 was skipped and not counted.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Restored complete 0.6.0 release notes and added a reproducible coverage check. Verified 15/15 required subjects against the generated PR body, exact-base CI, and the published release.
<!-- SECTION:FINAL_SUMMARY:END -->
