---
id: CFO-0015
title: Cloudflare API drift canary
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
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
- [ ] #2 A deliberate contract edit makes it fail with a readable diff
- [ ] #3 The token never appears in logs or artefacts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
