---
id: CFO-0014
title: Helm chart
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - helm
dependencies: []
priority: low
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
charts/cf2otel plus helm.yml, publish.yml helm-chart-path, ghcr-cleanup chart package, and release-please extra-files, following rfc6035-2otel.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 helm lint, template and kubeconform (with CRD schemas, no -ignore-missing-schemas) pass in CI
- [ ] #2 Chart publishes to ghcr.io/rknightion/charts/cf2otel on release
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
