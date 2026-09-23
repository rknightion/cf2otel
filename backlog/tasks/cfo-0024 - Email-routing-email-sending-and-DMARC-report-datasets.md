---
id: CFO-0024
title: 'Email routing, email sending and DMARC report datasets'
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:2'
  - email
dependencies: []
priority: low
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Zone datasets emailRoutingAdaptive, emailSendingAdaptive, dmarcReportsAdaptive.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Check which zones have data before building
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
