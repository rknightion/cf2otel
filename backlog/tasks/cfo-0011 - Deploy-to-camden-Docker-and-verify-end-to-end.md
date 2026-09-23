---
id: CFO-0011
title: Deploy to camden (Docker) and verify end to end
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - deploy
dependencies: []
priority: high
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
deploy/docker-compose.yaml reference plus the live deployment: compose project under /opt/compose/cf2otel, data under /opt/cf2otel (config 0600 owned by 65532, state dir), secrets in a root-only .env. Image from GHCR via release-please. Docker only: no Helm install.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Container healthy on camden running a released ghcr.io/rknightion/cf2otel tag
- [ ] #2 Every wave-1 collector's signals are observed in the m7kni stack (Loki, Mimir, Tempo) with exact query evidence
- [ ] #3 Restart test: a container restart resumes from checkpoints without gaps or duplicates
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
