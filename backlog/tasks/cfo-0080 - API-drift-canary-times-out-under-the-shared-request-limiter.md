---
id: CFO-0080
title: API drift canary times out under the shared request limiter
status: Parked
assignee:
  - '@loop21-root'
created_date: '2026-10-04 22:21'
updated_date: '2026-10-04 22:25'
labels: []
dependencies: []
priority: high
type: bug
ordinal: 106000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Scheduled drift runs since 2026-10-03 16:19Z fail all GraphQL settings scopes. Prove or refute shared pacing exhausting the two-minute probe context. Scope tools/apidrift only, budget within ten-minute CI timeout; no workflow or cfapi changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Default-limiter realistic-contract fixture reproduces timeout on base and passes after a request-count-sized budget fix.
- [ ] #2 Diff errors carry sanitized deadline, rate-limit, HTTP status class or envelope class without IDs, credentials or response bodies.
- [ ] #3 just check and CodeRabbit pass; landed SHA CI green; one root-dispatched drift workflow succeeds or remaining differences are real drift with classes named.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop21: inspect scheduled failure evidence, realistic default-limiter red/green, contract-request-count probe budget and sanitized classification; gate/CodeRabbit, land first and exact CI, root dispatches one canary.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop21: implementation attempts0; read-only logs last-green37120423235 at597f2bfe versus first-red37136430124 at2eb6a689 support two-minute context exhaustion.31account datasets plus12zone datasets x23zones =307settings requests, minimum612seconds at0.5rps, beyond600second job before REST/discovery. No files changed, local Cloudflare calls, gate, CodeRabbit, push or workflow dispatch. Parked owner: recommend longer workflow timeout and contract-count budget; alternatively separately authorised batching. Frozen tools-only budget correction cannot meet acceptance.
<!-- SECTION:NOTES:END -->
