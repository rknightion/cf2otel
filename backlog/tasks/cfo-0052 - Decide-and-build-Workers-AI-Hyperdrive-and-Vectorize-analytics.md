---
id: CFO-0052
title: 'Decide and build Workers AI, Hyperdrive and Vectorize analytics'
status: In Progress
assignee:
  - '@loop16-root'
created_date: '2026-09-30 21:17'
updated_date: '2026-10-02 14:30'
labels:
  - platform
dependencies: []
priority: low
type: spike
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Newer developer-platform products have GraphQL datasets no comparable exporter covers yet. Record a per-dataset build/skip decision as CFO-0023 did, then build the chosen ones.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A per-dataset build/skip decision with reason is recorded
- [ ] #2 Chosen datasets are built
- [ ] #3 A new dataset or endpoint has a live-verified entry in doc-0003 and an API drift canary probe
- [ ] #4 Every new signal and attribute is declared in internal/semconv, listed in docs/signals.md, and has a panel in grafana/build_dashboard.py
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop16 chooses aiInferenceAdaptiveGroups for aggregate metrics, not sampled raw-row traffic counts. Root verified schema descriptions and an exact live selection for count, five-minute timestamp and input/output token and inference-time sums. Hyperdrive and Vectorize datasets returned zero rows in seven days and are skipped. No separate raw log collector is chosen. Initial collector stays opt-in and aggregate-only with existing platform series cap, no model or resource identifiers as metric labels.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC1 decided from one settings read and seven-day limit-one existence queries per advertised dataset: both Workers AI sources eligible, Groups chosen for corrected aggregates; raw alternative skipped as redundant for metrics and raw-log expansion not chosen. Eight Hyperdrive/Vectorize sources empty and skipped. Count one is a lower bound, zero is exact. Live schema describes count as total inferences, input/output token sums and total inference time in milliseconds; exact selected fields returned numeric nonnull values. No backend delivery proof or ingestion-lag guarantee is claimed.
<!-- SECTION:NOTES:END -->
