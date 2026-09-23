---
id: CFO-0002
title: 'Cloudflare API client: read-only REST + GraphQL with entitlement-aware queries'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - cfapi
dependencies: []
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/cfapi. The only package that talks to Cloudflare. See doc-0003 for the live-verified contract and traps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Any method other than GET, or POST to /graphql, is refused before network I/O, and a test proves it
- [ ] #2 Retries 429/502/503/504 honouring Retry-After, else bounded exponential backoff; caps response size; sets timeouts; wraps the transport for cf2otel.* HTTP client metrics and spans
- [ ] #3 GraphQL selections are built from settings.availableFields intersected with the wanted fields, split to respect maxNumberOfFields, and windows are split to respect maxDuration and clamped to notOlderThan
- [ ] #4 Entitlement errors are matched case-insensitively and cause a field-set renegotiation, not a crash loop
- [ ] #5 Zone and gateway discovery via GET /zones and /ai-gateway/gateways when no list is configured
- [ ] #6 Fixtures under internal/cfapi/testdata are sanitized per the wave operating model
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
