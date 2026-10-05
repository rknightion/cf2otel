---
id: CFO-0080
title: API drift canary times out under the shared request limiter
status: Done
assignee:
  - '@loop22-root'
created_date: '2026-10-04 22:21'
updated_date: '2026-10-05 12:29'
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
- [x] #1 Default-limiter realistic-contract fixture reproduces timeout on base and passes after a request-count-sized budget fix.
- [x] #2 Diff errors carry sanitized deadline, rate-limit, HTTP status class or envelope class without IDs, credentials or response bodies.
- [x] #3 just check and CodeRabbit pass; landed SHA CI green; one root-dispatched drift workflow succeeds or remaining differences are real drift with classes named.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop21: inspect scheduled failure evidence, realistic default-limiter red/green, contract-request-count probe budget and sanitized classification; gate/CodeRabbit, land first and exact CI, root dispatches one canary.

Loop22 based on M2 landed15c7ef31: realistic per-class default-limiter contract-count fixture red, size tool probe context from REST/GraphQL counts/rates plus retry/latency within existing10minute job; sanitized error classes. Gate and CodeRabbit, root independent review before landing; exactCI. Root single live workflow dispatch only after final L1 observation, never during it.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop21: implementation attempts0; read-only logs last-green37120423235 at597f2bfe versus first-red37136430124 at2eb6a689 support two-minute context exhaustion.31account datasets plus12zone datasets x23zones =307settings requests, minimum612seconds at0.5rps, beyond600second job before REST/discovery. No files changed, local Cloudflare calls, gate, CodeRabbit, push or workflow dispatch. Parked owner: recommend longer workflow timeout and contract-count budget; alternatively separately authorised batching. Frozen tools-only budget correction cannot meet acceptance.

Loop22 implementationattempt1: realdefault M2baseline120stimeout98requests; candidate308GraphQLcontractsettings+singleton allpass.104boundedREST+308GraphQL budget512.267s includes contingency; oversizedfailclosed, fixedsanitizedclasses. justcheck and uncachedtools gateexit0, CodeRabbit4filescomplete0findings, independentreviewPASS. Landed3e85282a6b91250ec592fcb7344bb77979d64f04 exactCI37305970420 all6jobs success; D1-composed justcheckexit0. Evidence /tmp/d1-acceptance-landed.json and /tmp/d1-landing-linkage.json. AC3 remains open until root singleworkflowdispatch AFTER fencedL1ends; fullCLI/startupreserve sustainedupstreamdelay unverified until that run. No workflow changes/localCloudflarecalls.

Loop22 attempt1complete: root singleauthorised driftworkflow37308881485 at finalSHA3e85282a6b91250ec592fcb7344bb77979d64f04 probejobcompleted success12:28:07UTC, actualoutput Cloudflare API contract matched at12:28:03.932. RunafterL1parked/noactiveobservation. LiveCLI now exercised within existing10minutejob, no workflowchange/retry. Existingdocumented-only lb-pool-health/dex-http-results/dex-traceroute-results remainunprobed; one GraphQLzone firewallEventsAdaptiveGroups selectionzero-rows accepted/value-shapeunproven. No genuine drift difference. Allthreecriteria nowmet, local/securityreview-equivalent routineproofreview/fullcomposed/exactCI alreadygreen.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Request-class/contract-sized probe budget and fixed sanitized errors; authentic120second baseline timeout and307settings+singletongreen, fullgate/CodeRabbit/independentreview/composed/exactCI green. Rootsingleliveworkflow37308881485 on3e85282a completed successfully with Cloudflare API contract matched; documented-only/zero-row coverage limitations retained.
<!-- SECTION:FINAL_SUMMARY:END -->
