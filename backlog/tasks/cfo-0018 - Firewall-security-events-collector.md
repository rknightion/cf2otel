---
id: CFO-0018
title: Firewall / security events collector
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:2'
  - firewall
dependencies: []
priority: medium
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
firewallEventsAdaptive raw events plus metrics; doc-0004 firewall section field superset.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Parity with doc-0004 firewall section
- [ ] #2 Severity mapped to OTel SeverityNumber
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
