---
id: CFO-0015
title: Cloudflare API drift canary
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 07:31'
labels:
  - 'wave:1'
  - ci
dependencies: []
priority: low
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Scheduled workflow reading KV secret/rknightion/cf2otel (role rknightion-cf2otel, schema-only token) that diffs GraphQL settings.availableFields / dataset limits and REST field shapes against committed spec files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Workflow runs green on main on schedule and on dispatch
- [x] #2 A deliberate contract edit makes it fail with a readable diff
- [x] #3 The token never appears in logs or artefacts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC1/3: dispatch run 35867543195 succeeded and drift unit test detects changed contract, but no scheduled invocation has occurred. Credential absence from every log/artefact was not established as an exhaustive proof. Resume at first scheduled run and inspect redacted logs/artifacts.

Wave 2 exhaustive download check: runs 35863371244, 35865194472, 35865604985, 35867543195 and 35893111445 each published zero artifacts; each downloaded log has zero occurrences of both schema-probe and runtime token bytes. AC1 still awaits first scheduled run at 06:17 UTC.

2026-09-24 P9: watcher ran from 06:23 to its 07:30 UTC deadline after the 06:17 cron slot; direct workflow history showed zero schedule-triggered Cloudflare API drift runs and only the five earlier 2026-09-23 workflow_dispatch runs. AC1 remains open: a successful dispatch does not prove the scheduled invocation. Resume on the first schedule-triggered run, require success at its exact SHA, then download and scan its logs and artifacts for both token byte strings before checking AC1.
<!-- SECTION:NOTES:END -->
