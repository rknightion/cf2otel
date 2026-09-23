---
id: CFO-0018
title: Firewall / security events collector
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 21:22'
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
- [x] #1 Parity with doc-0004 firewall section
- [x] #2 Severity mapped to OTel SeverityNumber
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: implement entitled firewall events and Groups-derived metrics with severity mapping.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Entitlement-aware firewall raw events, Groups metrics and OTel severity mapping verified by tests, just check/ci at a3fa01b, and v0.2.2 live Loki/Mimir observations. Free ByTimeGroups rejects advertised action/source dimensions; doc-0003 and signals.md record the count-only fallback.

Final wave 2 gate: missing configured zones now fail closed (red-first events/metrics test, go test -race, independent L5 re-review PASS, CodeRabbit zero findings). v0.2.3 merge SHA 935bf72 deployed healthy on the deployment host with the expected image digest; prior live Pro-zone 19:00-19:10 UTC census found 41 source rays and 41 distinct Loki rays, plus two firewall metric series in Mimir. just check and just ci passed at 8e66817 in cf2otel-verify.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented entitlement-aware raw firewall events and Groups-derived metrics with severity mapping, count-only Free fallback, and fail-closed configured-zone validation. Verified by focused race tests, just check/ci, independent review, 41/41 live Pro-zone rays and Mimir metric presence; released as v0.2.3.
<!-- SECTION:FINAL_SUMMARY:END -->
