---
id: CFO-0066
title: Restore AI Gateway catch-up through bounded OTLP delivery
status: In Progress
assignee:
  - loop14-root
created_date: '2026-10-01 10:05'
updated_date: '2026-10-01 11:07'
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
- [ ] #1 A bounded reproduction demonstrates the actual delivery blocker without weakening export-before-checkpoint integrity
- [ ] #2 The corrected path advances AI Gateway windows only after required log and trace exports succeed
- [ ] #3 A subsequent healthy deployment shows checkpoint advancement and fresh last-success telemetry with the stale alert cleared
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
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
<!-- SECTION:NOTES:END -->
