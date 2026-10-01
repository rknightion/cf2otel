---
id: CFO-0069
title: >-
  AI Gateway log delivery stalls again on trace export deadlines after adaptive
  window halving
status: To Do
assignee: []
created_date: '2026-10-01 17:50'
labels:
  - aigateway
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On 0.10.1 the aigateway.logs collector delivered for about 90 minutes after deploy and then stopped: no success since about 15:10Z on 2026-10-01, with "aggregate commit budget exhausted" errors as the source window halved from 15m to 7m30s to 3m45s (traces: context deadline exceeded) at roughly one failure per fifteen to thirty minutes and no success in between. Loop 14 accepted the fix on a two-read checkpoint advance thirteen minutes after deploy. The exclusive cause of the OTLP trace export deadline was never proven; the aggregate 90-second commit budget, per-span body size and the trace endpoint's own limits are all candidates. Halving one step per failed cycle converges too slowly to be a recovery mechanism, and the stale-collector alert fires again.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The cause of the trace export deadline is established from live evidence (per-commit span count, bytes and exporter response), not inferred from the timeout
- [ ] #2 A reproduction through the scheduler, provider and a real HTTP OTLP collaborator fails by assertion on the released code
- [ ] #3 A window whose trace payload cannot be delivered inside the commit budget converges to a deliverable size within one scheduled cycle, exporting before the checkpoint moves and without dropping or truncating required signals silently
- [ ] #4 After deploy the collector's last-success age stays under 15 minutes for 60 continuous minutes and the stale alert instance is Normal
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
