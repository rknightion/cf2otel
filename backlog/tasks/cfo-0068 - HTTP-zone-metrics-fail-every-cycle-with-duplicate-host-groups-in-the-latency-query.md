---
id: CFO-0068
title: >-
  HTTP zone metrics fail every cycle with duplicate host groups in the latency
  query
status: To Do
assignee: []
created_date: '2026-10-01 17:50'
updated_date: '2026-10-01 18:41'
labels:
  - http
  - telemetry
dependencies: []
priority: high
type: bug
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Since the 0.10.0 deploy on 2026-10-01 the httpreq.metrics collector has succeeded once (its first cycle) and failed every five-minute cycle after it with "HTTP latency query returned duplicate host groups" (internal/collectors/httpreq/latency.go). The request, bytes, breakdown and latency series have had no new data since; 0.10.1 has never recorded a success for this collector. Loop 14's live proof read the series created by that single first cycle and passed. A fifteen-minute live read of host groups with the collector's eyeball filter showed no two raw host values collapsing to one normalised host, so normalisation is not the proven cause; the leading unproven hypothesis is that the GraphQL client concatenates one row set per maxDuration sub-window when a retained checkpoint makes the window longer than one sub-window, which also explains why the failure is self-sustaining.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reproduction through the registered collector with a real HTTP collaborator fails by assertion on the released code and names the actual cause of the duplicate host groups
- [ ] #2 One host appearing in several source groups for a window no longer fails the collector, and latency percentiles are never averaged or summed across groups
- [ ] #3 A latency failure for one zone cannot stop request, bytes and breakdown totals for the window without that loss being visible in self-observability
- [ ] #4 After deploy the collector records a success on every cycle for 60 minutes with no scrape error, and the request counter increases
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Cause proven live at loop 15 preparation (2026-10-01, runtime token, eyeball filter): the dataset returns the raw Host header, so a client sending name:port creates a second source group that normalises to the same host. Over ten hours 4 of 23 zones had such groups (17 port-suffixed groups with 31 requests beside 7 canonical groups with 3,239), one zone inside a single five-minute window. The sub-window hypothesis in the description is wrong: maxDuration is 30 days on every zone, so the client never splits these windows.
<!-- SECTION:NOTES:END -->
