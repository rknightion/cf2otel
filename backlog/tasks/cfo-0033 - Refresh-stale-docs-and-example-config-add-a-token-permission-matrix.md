---
id: CFO-0033
title: 'Refresh stale docs and example config, add a token permission matrix'
status: To Do
assignee: []
created_date: '2026-09-25 12:07'
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
- [ ] #1 No doc describes the collector set as wave-1 only or calls shipped domains planned
- [ ] #2 A test fails if config.example.yaml omits a registered collector name or the chart values omit a config section internal/config defines
- [ ] #3 docs/security.md maps each collector to its Cloudflare permission group, marking anything not live-verified as unverified
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
