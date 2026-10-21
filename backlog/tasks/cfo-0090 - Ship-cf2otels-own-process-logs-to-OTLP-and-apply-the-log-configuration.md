---
id: CFO-0090
title: Ship cf2otel's own process logs to OTLP and apply the log configuration
status: Done
assignee:
  - '@claude'
created_date: '2026-10-21 03:51'
updated_date: '2026-10-21 04:17'
labels:
  - bug
dependencies: []
priority: high
ordinal: 120000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cf2otel's slog output (including 'collector run failed' with the error) goes only to the container's stdout in Go's default text format. Watchtower recreates the container on every deploy, and docker logs covers only the current container, so the cause of every failed poll during the 2026-10-21 rollouts was lost (see CFO-0085 and CFO-0089). log.level and log.format exist in the configuration with defaults info/json, but nothing applies them: there is no slog.SetDefault.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 log.level and log.format configure the process logger; an invalid value is rejected at load
- [x] #2 Every process log record at or above the configured level is also exported over OTLP as event cf2otel.process.log with its severity, on a pipeline separate from checkpoint commits (its failures never fail a commit)
- [x] #3 The event is declared in internal/semconv and listed in docs/signals.md
- [ ] #4 Live: a collector failure line is queryable in Loki with {service_name="cf2otel"} | event_name="cf2otel.process.log"
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in c6b90ac (pushed). Red first: config test accepted invalid level/format; tee test failed to build without the handler. NewLogHandler applies log.level/log.format (case-insensitive) to stderr; Validate rejects other values. ProcessLogHandler tees every enabled record to a separate batched LoggerProvider as cf2otel.process.log with severity and a JSON body; it is outside the commit export observer. The default logger reverts to stderr-only before provider shutdown (CodeRabbit minor finding, fixed). Docs: signals.md event and inventory rows, configuration.md dedupe row. Remote gate 20261021T035644Z-cf2otel-5f63992e just check exit 0 (before the 3-line shutdown-order fix, which passed vet and cmd tests). Deployed host config has no log section, so defaults info/json apply. AC4 live Loki readback pending.

Live 2026-10-21 04:15 UTC: the c6b90ac process's startup records arrived in Loki under {service_name="cf2otel"} | event_name="cf2otel.process.log" within a minute, JSON-formatted with level and msg. No collector failure occurred to observe, so AC4 stays unchecked; the owner closed the task with the shared export path proven. DoD2 not applicable: no Dockerfile, goreleaser or image change.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
cf2otel now applies log.level and log.format (invalid values rejected at load) and exports every process log record over OTLP as cf2otel.process.log on a batched pipeline separate from checkpoint commits, so failure causes survive container recreates. Verified by red-then-green unit tests, a passing remote just check, CodeRabbit (one minor shutdown-order finding fixed) and live Loki readback of startup records. Not observed live: an actual collector failure record (AC4); it uses the same path. Durations appear as nanosecond integers in the JSON format.
<!-- SECTION:FINAL_SUMMARY:END -->
