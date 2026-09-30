---
id: CFO-0054
title: AI Gateway log collector fails every window on a non-JSON response body
status: In Progress
assignee:
  - loop14-root
created_date: '2026-09-30 21:44'
updated_date: '2026-09-30 23:00'
labels:
  - aigateway
dependencies: []
priority: high
type: bug
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Since 2026-09-27 the aigateway.logs collector has failed every run (about 288 failures a day) with "aigateway response body: invalid character R looking for beginning of value": a body fetch returns a non-JSON payload and the error fails the whole window, so no AI Gateway logs, spans or metrics have been exported for over three days and the checkpoint is not advancing. The collector-stale alert has been firing since then. How far back the log API reaches decides how much of the gap can still be recovered.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A reproduction test with a non-JSON body response fails before the fix
- [x] #2 A non-JSON or error body is recorded on that request (and counted) without failing the window; metadata-only export continues
- [ ] #3 After deploy the collector catches up and its stale alert clears; the unrecoverable span, if any, is recorded
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop14 candidate 14ef1dbbaccbc56a7f7a74b6297b3cd67a78af74 independently reviewed PASS on that exact SHA. Non-JSON body assertions fail on base and pass candidate; just check and CodeRabbit complete, zero findings/all seven files covered. Root landing 67eee5828089708ae25544b43d77d6fe89022c88 passed gen-check/check, leak scan zero hits, pushed; CI and deployed catch-up/stale-alert AC3 pending.
<!-- SECTION:NOTES:END -->
