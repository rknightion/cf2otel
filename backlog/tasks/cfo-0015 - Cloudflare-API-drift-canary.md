---
id: CFO-0015
title: Cloudflare API drift canary
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:14'
labels:
  - 'wave:1'
  - ci
dependencies: []
priority: low
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Scheduled workflow reading KV secret/rknightion/cf2otel (role rknightion-cf2otel, schema-only token) that diffs GraphQL settings.availableFields / dataset limits and REST field shapes against committed spec files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Workflow runs green on main on schedule and on dispatch
- [x] #2 A deliberate contract edit makes it fail with a readable diff
- [ ] #3 The token never appears in logs or artefacts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC1/3: dispatch run 35867543195 succeeded and drift unit test detects changed contract, but no scheduled invocation has occurred. Credential absence from every log/artefact was not established as an exhaustive proof. Resume at first scheduled run and inspect redacted logs/artifacts.
<!-- SECTION:NOTES:END -->
