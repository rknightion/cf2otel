---
id: CFO-0043
title: Restore actionlint after Renovate selected ubuntu-26.04
status: Done
assignee: []
created_date: '2026-09-27 09:29'
updated_date: '2026-09-27 12:22'
labels:
  - ci
dependencies: []
priority: low
type: bug
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Renovate commit 973ef19 changed publish.yml to ubuntu-26.04. At main 6a40d21, actionlint run 36297746270 failed because its runner-label table does not recognize ubuntu-26.04; CI itself succeeded. Determine whether the label is runnable and choose a compatible runner pin or update the validator without weakening validation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The publish workflow uses an available runner label and actionlint passes at the resulting main SHA
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 11: CI43-a1 implementation 1/4; review-repair 0/3; infrastructure retries 0. Grant: publish.yml runner label only. main 2b23114ec3fe8dca5a32734360340669f9cf40cc: local actionlint and exact-SHA just check passed; actionlint run 36314561944 and CI 36314561661 including ci-success succeeded; all workflows at that SHA succeeded. CodeRabbit skipped for declarative CI YAML. DoD 2 and 3 are not applicable: no image build definition or new signal changed.
<!-- SECTION:NOTES:END -->
