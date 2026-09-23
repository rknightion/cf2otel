---
id: CFO-0013
title: Documentation site and hub onboarding
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
labels:
  - 'wave:1'
  - docs
dependencies: []
priority: medium
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
zensical docs (docs.toml, docs/), trigger-docs-sync.yml (role docs-sync-cf2otel is provisioned), and onboarding in m7kni/m7kni-net-site: docs-repos.json entry plus the brand assets its fleet-guard tests require (read its reference/brand.md).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 docs cover getting started, configuration, env vars (generated), signals, security/PII, troubleshooting and a comparison page
- [x] #2 m7kni-net-site just check passes with cf2otel onboarded, and m7kni.io/cf2otel/ serves the docs
- [x] #3 trigger-docs-sync.yml runs green on main
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Hub just check passed 253 tests, CI 35863528251 succeeded, docs URL returned HTTP 200, and docs-sync at source SHA 508c929 succeeded.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documentation and hub onboarding published; hub CI, docs sync and live URL verified.
<!-- SECTION:FINAL_SUMMARY:END -->
