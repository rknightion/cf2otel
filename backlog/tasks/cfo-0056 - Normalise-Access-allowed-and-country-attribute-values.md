---
id: CFO-0056
title: Normalise Access allowed and country attribute values
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
labels:
  - access
dependencies: []
priority: medium
type: bug
ordinal: 81000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cloudflare.access.allowed is the string allowed on cloudflare_access_logins_total (GraphQL dimension passed through raw) but true/false on the identity-login and request metrics, and cloudflare.access.country is lowercase on login logs while every other country attribute is an uppercase ISO code. Queries have to special-case both.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 allowed is a boolean-valued true/false on every Access signal
- [ ] #2 Access country values are uppercase ISO 3166-1 alpha-2
- [ ] #3 docs/signals.md and the dashboard queries match
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
