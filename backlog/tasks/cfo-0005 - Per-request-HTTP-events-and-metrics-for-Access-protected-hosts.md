---
id: CFO-0005
title: Per-request HTTP events and metrics for Access-protected hosts
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - http
dependencies: []
priority: high
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
httpRequestsAdaptive (sampled raw events, 31d) and httpRequestsAdaptiveGroups for the hostnames of Access applications. Scope is configurable; the default is Access-protected hosts only (decision 2026-09-23, Rob).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Raw events become log records with every entitled field; the selection is negotiated from availableFields per zone
- [ ] #2 Request, status-class, cache and origin-latency metrics come from the Groups dataset, never from counting raw rows
- [ ] #3 Scope config supports access_protected (default), explicit host list, and all hosts per zone
- [ ] #4 Verified live: events for an Access-protected host land in Loki with host, path, status and client fields
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
