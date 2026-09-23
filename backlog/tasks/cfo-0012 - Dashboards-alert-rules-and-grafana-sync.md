---
id: CFO-0012
title: 'Dashboards, alert rules and grafana-sync'
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
labels:
  - 'wave:1'
  - grafana
dependencies: []
priority: medium
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Generated dashboard and alert rules synced through gc-gitsync-m7kni via grafana-sync.yml (role gitsync-cf2otel is provisioned).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Dashboard covers Access logins (identity vs service token), HTTP on protected hosts with inferred identity, AI Gateway usage/cost/latency/errors, and collector health
- [x] #2 Alert rules for collector staleness and export failure
- [x] #3 grafana-sync.yml runs green on main and the dashboard renders on the m7kni stack
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
grafana-sync run 35867473592 succeeded; dashboard resource and both alert rules read back. Grafana server render returned HTTP 200 image with visible Access panels.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Dashboard and two alerts synced; render and API readback verified.
<!-- SECTION:FINAL_SUMMARY:END -->
