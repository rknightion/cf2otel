---
id: CFO-0026
title: Deduplicate Access identity observations on window replay
status: To Do
assignee: []
created_date: '2026-09-23 17:52'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Window delivery retries after an OTLP export failure, while access.logins observes identities in memory during collection. The current identity index accepts the same login ray again on each retry, consuming candidate capacity and potentially changing match behavior. This was found during Wave 2 review; internal/identity is outside that wave.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Replaying the same Access login row after a failed export leaves one identity candidate and consumes capacity once.
- [ ] #2 Distinct login rows remain individually matchable, including rows with the same timestamp.
- [ ] #3 A regression test exercises the retry path through the collector and scheduler.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
