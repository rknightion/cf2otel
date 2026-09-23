---
id: CFO-0005
title: Per-request HTTP events and metrics for Access-protected hosts
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
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
- [x] #1 Raw events become log records with every entitled field; the selection is negotiated from availableFields per zone
- [x] #2 Request, status-class, cache and origin-latency metrics come from the Groups dataset, never from counting raw rows
- [x] #3 Scope config supports access_protected (default), explicit host list, and all hosts per zone
- [x] #4 Verified live: events for an Access-protected host land in Loki with host, path, status and client fields
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
HTTP scope and Groups tests passed in exact CI 35875578089. Loki query {service_name="cf2otel"} | event_name="cloudflare.http.request" returned 107 live rows with host, path, status and client metadata.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Protected-host HTTP events and Groups metrics released and observed live in Loki/Mimir.
<!-- SECTION:FINAL_SUMMARY:END -->
