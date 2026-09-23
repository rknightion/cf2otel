---
id: CFO-0010
title: 'Self-observability, health endpoint and CLI modes'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - ops
dependencies: []
priority: medium
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Parity floor from doc-0004: per-collector poll counters, durations, last-success timestamps, cursor lag, export outcomes, build info; /healthz on loopback; CLI modes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Flags: -config, -version, -healthcheck, -validate, -print-effective-config, -once, -dry-run, -since/-before window override, -datasets selection, -reset-state, and an explore mode that prints a dataset and names a missing permission on 403
- [ ] #2 Poll interval clamped to a floor; one failing collector never stops the others
- [ ] #3 Config-file permission advisory (0600, owned by the runtime UID) as in tailscale2otel
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
