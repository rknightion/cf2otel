---
id: CFO-0040
title: Harden AI Gateway DLP and coverage tests after loop 8 review
status: Done
assignee: []
created_date: '2026-09-26 18:17'
updated_date: '2026-09-26 18:43'
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
- [x] #1 DLP tests assert the log event's attributes including dlp.profiles, and cover duplicate directions and policy ids across findings
- [x] #2 A null dlp_profiles element is skipped, with a test
- [x] #3 A -since/-before range over aigateway.coverage either exports every closed window or reports that it exported only the first, with a test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 8 L40 (b4b755e, 5b6e8ab, 43b14ee, f5e7139): null dlp_profiles elements are skipped (all-null counts once with direction other); DLP tests assert the log event's attributes including dlp.profiles and cover duplicate directions and policy ids across findings; a -since/-before range over aigateway.coverage spanning more than one closed window is refused with an error before any read or export, documented in docs/signals.md. AC2/AC3 tests fail on the base, AC1 regressions proved by breaking the code. Gate green, CodeRabbit 0 findings, independent REV-L40 PASS (decode equivalence over 16 shapes; scheduler catch-up unaffected). Landed in dfc5de603ee5b3b50937ec319aeda43b0e8778bf; CI 36263040943 success incl ci-success. Loop 8: implementation 1/4, review-repair 0/3, infrastructure retries 0; grant: priority-7 defect fix in accepted work; reason Done.
<!-- SECTION:NOTES:END -->
