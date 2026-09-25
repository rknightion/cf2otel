---
id: CFO-0028
title: Make -dry-run side-effect free and validate -datasets
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-25 14:30'
labels:
  - bug
dependencies: []
priority: high
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
-dry-run only swaps in a no-op emitter (cmd/cf2otel/main.go:85); the run still opens the real checkpoint FileStore and commits windows (internal/collector/scheduler.go:250), so a dry run advances checkpoints while exporting nothing. Pointed at the live state directory this permanently loses Access and SCIM data, because the Access REST log reaches back only about a day. -dry-run -reset-state also archives the live state file. -datasets is read only by the -once path and silently ignored by the daemon, and an unknown name matches nothing and exits 0. The doc-0004 parity floor requires dry run with no send and no state write, plus per-source counts. Decision (2026-09-25 loop 6 prep): daemon-mode dry run is rejected, not run as a single pass.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A test runs -dry-run -once against a temp state dir with a seeded checkpoint: the file is byte-identical afterwards and no OTLP request reaches an httptest endpoint; the test fails on the pre-fix code
- [x] #2 -dry-run without -once or -since/-before is rejected by argument parsing, and -dry-run -reset-state is rejected, each with a test
- [x] #3 An unknown -datasets name fails with an error listing valid collector names, and -datasets in daemon mode is rejected rather than ignored
- [x] #4 Dry run prints a per-collector record and metric count, and the flag semantics are documented in docs/getting-started.md or docs/troubleshooting.md
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 6: protect the checkpoint store during a bounded dry run, reject incompatible flag combinations and unknown collectors, then verify counts and no export.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
L28 candidate 13353e2904833c50614ff9b5f10bb6be3ccc654f landed in 7a7bbdd0986cd6cc60d51823887503c7e655402c. The seeded-checkpoint integration test failed on the base and passed after the fix; it verifies byte-identical state and zero OTLP requests. The CLI tests cover invalid dry-run and dataset modes. The integrated test asserts two records and three collector metrics. just check passed at 7a7bbdd; all nine workflow runs at that SHA concluded success, including ci-success.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Dry run now preserves checkpoint bytes and sends no OTLP; invalid flag combinations and unknown datasets fail. Verified by the red-then-green integration and CLI tests, just check, CodeRabbit, and exact-SHA CI at 7a7bbdd.
<!-- SECTION:FINAL_SUMMARY:END -->
