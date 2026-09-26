---
id: CFO-0039
title: Allow environment overrides for dotted collector names
status: To Do
assignee: []
created_date: '2026-09-26 18:17'
labels:
  - config
  - bug
dependencies: []
priority: low
ordinal: 39000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Collector names contain dots (aigateway.logs, aigateway.coverage, email.routing). koanf uses . as its key delimiter, so CF2OTEL_COLLECTORS__AIGATEWAY.COVERAGE__ENABLED=true fails with 'collectors[aigateway]' has invalid keys: coverage. The same happens for every dotted collector on v0.6.0, so a per-collector switch can only be set in YAML. Found by the CFO-0038 lane in loop 8.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A CF2OTEL_ environment variable can set enabled, interval, initial_lookback and max_window for a dotted collector name, with a test
- [ ] #2 docs describe the environment form for per-collector settings
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
