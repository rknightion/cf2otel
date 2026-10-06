---
id: CFO-0083
title: Export process start time as a self-metric
status: To Do
assignee: []
created_date: '2026-10-06 16:37'
labels:
  - telemetry
dependencies: []
priority: medium
type: enhancement
ordinal: 112000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
In-place auto-RC restart retains stale old-process series sharing instance identity with a different version; explicit process start enables unambiguous lifetime-fenced proof. Owner authorized this follow-up after CFO-0082 (Upgrade OpenTelemetry Go to the v1.47 line) manual acceptance.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 cf2otel.process.start_time has unit s and description Unix time the cf2otel process started.; declared in semconv selfobs and metadata, captured once and emitted beside build.info.
- [ ] #2 Update /Users/rob/repos/cf2otel/docs/signals.md and required generated files; introduce no other signals or attributes.
- [ ] #3 Existing telemetry receiver regression observes the real export and fails on base for the missing signal, passes candidate; no tests weakened.
- [ ] #4 Update /Users/rob/repos/cf2otel/LOOP.md auto-RC trap: full label epoch build_commit plus process-start plus exactly one instance; file remains <6KB.
- [ ] #5 just check and CodeRabbit complete with critical/major findings resolved; exact-SHA ci-success green.
- [ ] #6 Root post-auto-RC GET-only proof before Done matches accepted SHA and exactly one process-start series later than run start and not future.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
