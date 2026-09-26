---
id: CFO-0040
title: Harden AI Gateway DLP and coverage tests after loop 8 review
status: To Do
assignee: []
created_date: '2026-09-26 18:17'
labels:
  - ai-gateway
  - test
dependencies: []
priority: low
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Independent reviews in loop 8 passed CFO-0036 and CFO-0038 with low-severity test gaps: the DLP test asserts span attributes but not the log-event record; nothing pins the restored cloudflare.ai_gateway.dlp.profiles attribute; no case has two findings with the same direction or duplicate policy ids; a null element inside dlp_profiles yields an extra other direction and count; the coverage collector's -since/-before range exports only its first closed window and still reports success.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 DLP tests assert the log event's attributes including dlp.profiles, and cover duplicate directions and policy ids across findings
- [ ] #2 A null dlp_profiles element is skipped, with a test
- [ ] #3 A -since/-before range over aigateway.coverage either exports every closed window or reports that it exported only the first, with a test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
