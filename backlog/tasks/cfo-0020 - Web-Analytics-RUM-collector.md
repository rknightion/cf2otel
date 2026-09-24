---
id: CFO-0020
title: Web Analytics / RUM collector
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 07:22'
labels:
  - 'wave:2'
  - rum
dependencies: []
priority: low
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
rumPageloadEventsAdaptiveGroups and rumWebVitalsEventsAdaptive(Groups) with ingestion-lag hold-back (doc-0004).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Parity with doc-0004 RUM section
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Collect account-scope pageload Groups counters and rolling Web Vitals p75 gauges with ten-minute holdback and stale-series clearing; verify the live route after deployment.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
RUM pageload counters and six rolling Web Vitals p75 gauges are registered and documented. Tests cover holdback, sentinel handling, stale clearing after export failure and bounded dimensions; the advertised quantiles field normalization passed a prechange-red contract test and CodeRabbit review. v0.3.1 exact-SHA just ci, CI and release passed; after deployment the Web Vitals checkpoint advanced and cloudflare_rum_lcp_p75 had a nonzero m7kni series. GraphQL wire-unit conversion remains an inference from samples and documentation.
<!-- SECTION:FINAL_SUMMARY:END -->
