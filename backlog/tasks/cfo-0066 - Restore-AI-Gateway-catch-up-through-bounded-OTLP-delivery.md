---
id: CFO-0066
title: Restore AI Gateway catch-up through bounded OTLP delivery
status: Done
assignee:
  - loop14-root
created_date: '2026-10-01 10:05'
updated_date: '2026-10-01 13:55'
labels:
  - aigateway
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 90000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 deployed v0.10.0 passes container, digest and checkpoint-preservation health checks, and the non-JSON parsing failure is gone. Live AI Gateway acceptance still fails: the log checkpoint remains at the outage boundary and the collector reports log/trace context deadlines during export. Runtime config permits a three-hour AI Gateway window. This is a live delivery blocker, not evidence that the original parser regression remains.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A bounded reproduction demonstrates the actual delivery blocker without weakening export-before-checkpoint integrity
- [x] #2 The corrected path advances AI Gateway windows only after required log and trace exports succeed
- [x] #3 A subsequent healthy deployment shows checkpoint advancement and fresh last-success telemetry with the stale alert cleared
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Diagnose the sanitized deployed settings and exact timeout evidence against collector and real exporter/scheduler paths; obtain an assertion-based failing integration reproduction; fix only bounded delivery or window subdivision; run gate, CodeRabbit and independent review; root owns any further release/deploy/live verification.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Real Register/scheduler/provider/SDK reproduction failed the expected checkpoint assertion after the shared 90-second deadline despite healthy local HTTP export responses. Metadata-only capture-disabled control is diagnostic, not a configuration proposal. The effective window is already capped to 15 minutes, superseding the initial three-hour-window hypothesis. Root source readback reports 712 records in the failing 15-minute range; 20 sampled rows have 40 successful body reads, 549526 total captured upper-bound bytes, maximum body 37280 bytes, and maximum source GET latency 0.77 seconds. No explicit numeric SDK timeout/batch environment overrides exist. Individual live OTLP POST statuses, bytes and latency remain unobserved. Adaptive source-window correction is commissioned under the shared third implementation attempt, leaving one implementation attempt, with no arbitrary timeout/capture changes. The initial bounded live watch expired with checkpoint unchanged, zero last-success and stale Alerting; live acceptance remains unsatisfied.

Production-budget real Register/scheduler/provider/SDK integration proves assertion-red aggregate deadline limitation, failed cursor and metric preservation, and subsequent smaller-window required-signal success. Candidate 836cbe7 and integrated a768eb9 passed gate, complete six-file CodeRabbit coverage and fresh exact-integration REV; exact-SHA CI succeeded. Healthy DEP3 v0.10.1 and LIVE54 terminal proof at 13:53:48 show checkpoint advancing across two reads, last-success below 15 minutes and stale Normal. Exclusive live causality remains unproven, and late v0.10.0 recovery is retained. DoD2/3 are conditional and not applicable: no packaging definition or new signal declaration was introduced.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
AI Gateway source windows halve on typed aggregate commit deadlines, preserving whole-second boundaries, the 90-second budget, capture caps and required-export-before-metrics/checkpoint integrity. Dense one-second windows fail closed; bounds are learned per scheduler process. Local production-budget proof, independent review, gates, CodeRabbit, CI and deployed recovery proof passed.
<!-- SECTION:FINAL_SUMMARY:END -->
