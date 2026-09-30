---
id: CFO-0050
title: >-
  Origin and edge health: certificates, load balancers, health checks, status
  page
status: To Do
assignee: []
created_date: '2026-09-30 21:17'
labels:
  - parity
  - health
dependencies: []
priority: medium
type: feature
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Certificate expiry, load balancer pool/origin health, health check results and Cloudflare incident status are cheap, high-value alerting signals that other exporters provide and cf2otel does not. Parent of the health subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All subtasks are Done
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
