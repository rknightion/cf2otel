---
id: CFO-0054
title: AI Gateway log collector fails every window on a non-JSON response body
status: To Do
assignee: []
created_date: '2026-09-30 21:44'
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
- [ ] #1 A reproduction test with a non-JSON body response fails before the fix
- [ ] #2 A non-JSON or error body is recorded on that request (and counted) without failing the window; metadata-only export continues
- [ ] #3 After deploy the collector catches up and its stale alert clears; the unrecoverable span, if any, is recorded
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
