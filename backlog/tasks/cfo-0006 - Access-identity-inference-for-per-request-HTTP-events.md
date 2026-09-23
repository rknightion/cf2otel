---
id: CFO-0006
title: Access identity inference for per-request HTTP events
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 15:12'
labels:
  - 'wave:1'
  - http
  - access
dependencies: []
priority: medium
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Join HTTP events to Access identity logins on client IP + host + time window. Decision 2026-09-23 (Rob): infer, and always flag the inference.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Matched events carry cloudflare.access.user.email and cloudflare.access.identity.inferred=true plus the matching login ray id; unmatched events carry neither
- [x] #2 The join window and ambiguity rule (multiple candidate users) are configurable and tested, and an ambiguous match is left unattributed
- [x] #3 A metric counts matched, unmatched and ambiguous events
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Join ambiguity, expiry and disabled-mode tests passed in exact CI 35875578089. Live Mimir has matched, unmatched and ambiguous identity metric families.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bounded inferred-identity join and outcome metrics released; tests and live metric families verified.
<!-- SECTION:FINAL_SUMMARY:END -->
