---
id: CFO-0029
title: 'Declare metric units, descriptions and explicit histogram buckets'
status: In Progress
assignee:
  - '@rknightion'
created_date: '2026-09-25 12:07'
updated_date: '2026-09-25 12:56'
labels: []
dependencies: []
priority: medium
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/telemetry/emitter.go creates every instrument with a bare name: no unit, no description and no SDK View, so the seconds histograms (gen_ai.client.operation.duration, cf2otel.api.duration, cf2otel.scrape.duration) use the SDK default buckets (0, 5, 10, 25 ... 10000) and nearly every sample lands in the first bucket. spec/genai-mapping.md froze the GenAI token counters as unit {token}, which is not emitted. Decision (Rob, 2026-09-25): accept the Prometheus series renames that unit suffixes cause in Grafana Cloud, and update dashboards and alerts to the new names.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Units and descriptions are declared in internal/semconv and applied by the emitter; a real-SDK exporter test asserts the unit on one counter, one gauge and one histogram
- [ ] #2 gen_ai.client.operation.duration uses the GenAI semconv explicit bucket boundaries and the two cf2otel duration histograms use sub-second boundaries, asserted on exported data points
- [ ] #3 docs/signals.md lists every metric's unit, dashboard and alert expressions use the resulting Prometheus names, and just gen-check passes
- [ ] #4 After deploy, one renamed series per unit kind is observed on the m7kni stack by exact query, and the old-to-new name map is recorded in the task notes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 6 L29: declare units/descriptions/buckets, verify SDK output, generate transitional Grafana expressions, then live-query renamed series after deploy.
<!-- SECTION:PLAN:END -->
