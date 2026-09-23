---
id: CFO-0014
title: Helm chart
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
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
- [x] #1 helm lint, template and kubeconform (with CRD schemas, no -ignore-missing-schemas) pass in CI
- [x] #2 Chart publishes to ghcr.io/rknightion/charts/cf2otel on release
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Helm workflow 35875578897 succeeded; v0.1.3 release run 35875579146 published chart digest sha256:861e8b03646de9848296b0d14c09512681e829b52c024e5473b5b71555e3c8c9.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Helm chart validated and published with v0.1.3.
<!-- SECTION:FINAL_SUMMARY:END -->
