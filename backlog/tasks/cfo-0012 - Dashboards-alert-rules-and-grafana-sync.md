---
id: CFO-0012
title: 'Dashboards, alert rules and grafana-sync'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
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
- [ ] #1 Dashboard covers Access logins (identity vs service token), HTTP on protected hosts with inferred identity, AI Gateway usage/cost/latency/errors, and collector health
- [ ] #2 Alert rules for collector staleness and export failure
- [ ] #3 grafana-sync.yml runs green on main and the dashboard renders on the m7kni stack
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
