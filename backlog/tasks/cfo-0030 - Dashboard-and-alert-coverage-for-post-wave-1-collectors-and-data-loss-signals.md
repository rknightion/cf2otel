---
id: CFO-0030
title: Dashboard and alert coverage for post-wave-1 collectors and data-loss signals
status: To Do
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-25 12:07'
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
- [ ] #1 just gen output has a panel for firewall, DNS analytics, RUM page views, sessions and Web Vitals, Gateway DNS, audit, origin duration, GenAI tokens and duration, and cf2otel window gap, commit failures, API retries and requests by status class; inferred-identity panels keep the inferred flag
- [ ] #2 New alert rules fire on any window gap increase, any dropped-window commit failure, and Access logins checkpoint age over 12 hours; just gen-check passes
- [ ] #3 grafana-sync succeeds on main and a read-back of the m7kni stack shows the new panels and rules
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
