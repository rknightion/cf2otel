---
id: CFO-0082
title: Upgrade OpenTelemetry Go to the v1.47 line
status: To Do
assignee: []
created_date: '2026-10-06 10:02'
labels:
  - deps
  - telemetry
dependencies: []
priority: medium
type: task
ordinal: 111000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PR38 failed on the OpenTelemetry upgrade. Track a compatible release-set upgrade with API adaptation and exact-source validation rather than pinning back incompatible log APIs. Root owns post-auto-RC telemetry proof and finalization.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Read the PR38 failing log and upstream changelog through Firecrawl before implementation; record the compatibility evidence.
- [ ] #2 All go.opentelemetry.io/otel* modules move to the v1.47 release set: otel, metric, trace, sdk, sdk/metric and OTLP metric/trace exporters v1.47.0; otlplog exporters v0.23.0; prometheus v0.69.0; log and sdk/log use the versions required by otlplog v0.23.0, with API adaptation, not pinback.
- [ ] #3 No other version changes except named MVS requirements; go mod tidy is clean; internal/semconv diff is empty; no tests are weakened.
- [ ] #4 just check and exact-SHA ci-success are green.
- [ ] #5 Root verifies post-auto-RC telemetry build_commit matches the accepted SHA, every collector last-success age is <1800s within 30 minutes, failed-attempt delta is zero over the observation window, and fresh Loki logs are present. Long-interval collectors may be reread once at 45 minutes. Root owns telemetry proof and finalization.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
