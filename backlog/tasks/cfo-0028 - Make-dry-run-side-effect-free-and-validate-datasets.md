---
id: CFO-0028
title: Make -dry-run side-effect free and validate -datasets
status: To Do
assignee: []
created_date: '2026-09-25 12:07'
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
- [ ] #1 A test runs -dry-run -once against a temp state dir with a seeded checkpoint: the file is byte-identical afterwards and no OTLP request reaches an httptest endpoint; the test fails on the pre-fix code
- [ ] #2 -dry-run without -once or -since/-before is rejected by argument parsing, and -dry-run -reset-state is rejected, each with a test
- [ ] #3 An unknown -datasets name fails with an error listing valid collector names, and -datasets in daemon mode is rejected rather than ignored
- [ ] #4 Dry run prints a per-collector record and metric count, and the flag semantics are documented in docs/getting-started.md or docs/troubleshooting.md
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
