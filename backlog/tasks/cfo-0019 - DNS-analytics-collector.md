---
id: CFO-0019
title: DNS analytics collector
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 07:22'
labels:
  - 'wave:2'
  - dns
dependencies: []
priority: low
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
dnsAnalyticsAdaptive raw queries and Groups metrics per zone.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Parity with doc-0004 DNS section
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Query per-zone DNS adaptive raw events and Groups counts using advertised fields, with bounded metric attributes and live proof after deployment.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
DNS adaptive raw events and Groups metrics are registered and documented. Focused tests cover raw parity fields, half-open windows, entitlement limits, required fields and bounded attributes; v0.3.0 exact-SHA just ci and CI passed. After deployment, the exact DNS Loki query returned 410 rows and the DNS metric had nonzero live series.
<!-- SECTION:FINAL_SUMMARY:END -->
