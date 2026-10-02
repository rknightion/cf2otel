---
id: CFO-0077
title: Harden platform HTTP fixture synchronization
status: To Do
assignee: []
created_date: '2026-10-02 17:38'
labels: []
dependencies: []
priority: low
type: chore
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 CodeRabbit review of the platform canary repair raised test-only synchronization concerns for phase in the D1 cap fixture and restCalls in the D1 name fixture. The complete race gate passed, so no production race or failing reproduction is claimed; the findings remain useful targeted test-harness hardening work outside this delivery slice.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The two named HTTP fixtures use explicit safe synchronization for state shared between test control and server handlers.
- [ ] #2 Existing public boundary assertions for counts, names, caps and read-only calls remain unchanged and race-enabled fixture tests pass.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
