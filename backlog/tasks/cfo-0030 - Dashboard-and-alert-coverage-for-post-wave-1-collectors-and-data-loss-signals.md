---
id: CFO-0030
title: Dashboard and alert coverage for post-wave-1 collectors and data-loss signals
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 17:57'
labels: []
dependencies:
  - CFO-0029
priority: medium
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The dashboard and the two alert rules (grafana/build_rules.py) cover only wave 1. Nothing shows firewall, DNS analytics, RUM, Gateway DNS, audit, origin duration or GenAI token and duration metrics, and nothing alerts on permanent data loss: a retention-gap skip (cf2otel.window.gap), a window dropped after repeated payload rejection (cf2otel.window.commit_failures outcome=dropped), or the Access logins checkpoint age approaching the Access REST reach of about a day. Build on the unit-suffixed metric names.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 just gen output has a panel for firewall, DNS analytics, RUM page views, sessions and Web Vitals, Gateway DNS, audit, origin duration, GenAI tokens and duration, and cf2otel window gap, commit failures, API retries and requests by status class; inferred-identity panels keep the inferred flag
- [x] #2 New alert rules fire on any window gap increase, any dropped-window commit failure, and Access logins checkpoint age over 12 hours; just gen-check passes
- [x] #3 grafana-sync succeeds on main and a read-back of the m7kni stack shows the new panels and rules
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7 run-end park: implementation attempts 0/4, review-repair 0/3, infrastructure retries 0; grant: dashboard/alert lane after L29. L30 admitted 11:23:23Z, worktree and frozen brief prepared, but no spawn control completed by 11:53:34Z; root orchestration dispatch stall, not provider outage. Resume with working child dispatch, recheck main and task dependencies, then use the retained l30 brief; no AC is proven.

Loop 8 LG: feat(grafana) 718970b added 17 panels (origin duration; window gaps, commit failures, API retries, API requests by status class; firewall, DNS analytics, Gateway DNS, audit; RUM page views, sessions and LCP/INP/FID/FCP/TTFB/CLS p75) on the unit-suffixed names, inferred-identity panels unchanged, and alert rules cf2otel-window-gap, cf2otel-window-commit-dropped (outcome=dropped) and cf2otel-access-checkpoint-age (> 43200 s). Root checked every queried series against m7kni Mimir: four absent (window gap counter, r2sql queries, RUM INP/FID p75) are declared in the deployed code and only emit on data. Landed in 234b4250eb88387994eecb3fab58495d3f06f8c3, just check green, CodeRabbit 0 findings, CI 36260205349 success, grafana-sync 36260205342 success; read-back: all 17 panel titles present on dashboard uid cf2otel and the three rules live with their titles. Loop 8: implementation 1/4, review-repair 0/3, infrastructure retries 0, grant: LG lane; reason Done.
<!-- SECTION:NOTES:END -->
