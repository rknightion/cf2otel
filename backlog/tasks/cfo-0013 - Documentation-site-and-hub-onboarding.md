---
id: CFO-0013
title: Documentation site and hub onboarding
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
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
- [ ] #1 docs cover getting started, configuration, env vars (generated), signals, security/PII, troubleshooting and a comparison page
- [ ] #2 m7kni-net-site just check passes with cf2otel onboarded, and m7kni.io/cf2otel/ serves the docs
- [ ] #3 trigger-docs-sync.yml runs green on main
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
