---
id: CFO-0039
title: Allow environment overrides for dotted collector names
status: Done
assignee: []
created_date: '2026-09-26 18:17'
updated_date: '2026-09-26 23:26'
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
- [x] #1 A CF2OTEL_ environment variable can set enabled, interval, initial_lookback and max_window for a dotted collector name, with a test
- [x] #2 docs describe the environment form for per-collector settings
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 9 frozen packet L39: prove the dotted-name env form, preserve default enablement and YAML precedence, update generated documentation, then gate, review, land and release.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 9: implementation L39-a1 (1/4); review-repair rr1 (1/3); infrastructure retries 0. Candidate 5d6517d, landing f59ebbf, v0.8.1 release at 9a330c4. Exact landing SHA passed just check and just ci; CI 36269719502 and ci-success succeeded; CodeRabbit complete with 0 findings and 0 unreviewed files. Independent review found missing underscore-name duration assertions; rr1 fixed them, and root verified the sole correction. Grant: frozen dotted collector env mapping and generated docs. Reason Done: AC1-2 verified; no new signal or attribute was declared.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Dotted collector settings now accept the documented CF2OTEL_COLLECTORS__<NAME>__<SETTING> environment form with YAML precedence and unchanged default enablement. Tests, exact-SHA just check/just ci, CodeRabbit, CI and v0.8.1 release verified.
<!-- SECTION:FINAL_SUMMARY:END -->
