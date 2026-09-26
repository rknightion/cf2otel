---
id: CFO-0033
title: 'Refresh stale docs and example config, add a token permission matrix'
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 11:37'
labels: []
dependencies:
  - CFO-0024
priority: low
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
README.md, docs/index.md, docs/comparison.md and docs/getting-started.md still describe the wave-1 collector set; config.example.yaml lists 9 of about 36 collector keys with no platform section; the Helm values lack http.metrics_scope, platform and collectors; docs/security.md never lists the Cloudflare permission groups each collector needs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 No doc describes the collector set as wave-1 only or calls shipped domains planned
- [x] #2 A test fails if config.example.yaml omits a registered collector name or the chart values omit a config section internal/config defines
- [x] #3 docs/security.md maps each collector to its Cloudflare permission group, marking anything not live-verified as unverified
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7: implementation 8f728eb landed at e917d0c3e55b9b00ed0d3a62169eb619d71bd549. Exact-SHA just check and CI run 36239158717 (ci-success success) passed. No wave-1/planned wording remains in the four overview docs; TestPublishedExamplesCoverConfigSurface compares default collector keys and Config YAML sections; docs/security lists all 41 configured collector keys, with unverified groups marked.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Refreshed collector documentation, example YAML and Helm values, and added the permission matrix and a config-surface regression test. Verified by just check, a 41/41 matrix comparison and CI at e917d0c.
<!-- SECTION:FINAL_SUMMARY:END -->
