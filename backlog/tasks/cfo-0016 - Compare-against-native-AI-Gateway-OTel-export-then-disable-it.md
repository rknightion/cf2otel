---
id: CFO-0016
title: 'Compare against native AI Gateway OTel export, then disable it'
status: In Progress
assignee:
  - '@rknightion'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-25 12:56'
labels:
  - 'wave:1'
  - aigw
dependencies: []
priority: medium
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Decision 2026-09-23 (Rob): after the first deployment, compare cf2otel's GenAI output with Cloudflare's native cf.aig.request spans for the same requests, record gaps/improvements, then switch off ONLY the AI Gateway trace forwarding (Workers observability exports stay on). Root-only, using the Global API Key in ~/repos/chat-personal/cloudflare.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Side-by-side comparison for at least 20 matched requests recorded in the wave report, with every attribute the native span has present in cf2otel's
- [ ] #2 Native AI Gateway OTel export disabled with the pre-state captured, and Workers OTLP destinations verified unchanged
- [ ] #3 service.name=ai-gateway spans stop arriving in Tempo while cf2otel spans continue
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 5 P8: pass 60-minute exactness gate on running Camden binary, use corrected jobStatus-excluding comparator, then no-op/cutover/conditional rollback within three authorized Cloudflare writes and watch native/cf2otel traces.

Loop 6: prove v0.5.1 exact source-ID delivery before no-op and cutover PUTs; watch native and cf2otel traces with conditional rollback.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC2/3: 62 matched native/cf2otel requests carried all eight native attribute keys; model suffix and numeric cost agree. The native gateway still has one OTel export entry. Cutover was withheld because prior partial AI Gateway windows produced duplicates and W16 no-duplicate acceptance failed. No Cloudflare write was made; fix delivery atomicity and reverify before disabling.

Wave 2 P7 at v0.2.3, 2026-09-23 17:00-19:15 UTC: native 17 spans, cf2otel 14, 14 strict pairs with all eight native keys present. New-cutover gate needs 20 pairs plus P1/P2; P1 currently has only 13/16 exact-ID Tempo spans. Gateway OTel export remains enabled and no Cloudflare write was made.

Correction: the P1 three-span gap was a Tempo TraceQL search false negative. Direct trace readback finds 16/16 exact source-ID spans. The P7 search table still has only 14 strict pairs and the P2 gate is pending, so cutover remains withheld.

P7 direct-trace correction: Tempo search missed four correlated cf2otel traces. Adding their exact Loki content-log trace IDs and verifying each through direct Tempo trace readback gives 17 native, 18 cf2otel spans and 17 strict pairs in 17:00-19:15 UTC, with all eight native keys present on all 17 pairs. Still below the 20-pair cutover threshold; no gateway write.

Wave 2 expanded P7 on v0.2.3 after checkpoint 21:14:29Z: Tempo native query {resource.service.name="ai-gateway" && name="cf.aig.request"} and cf2otel query {resource.service.name="cf2otel" && span.gen_ai.operation.name="chat"}, 2026-09-23 17:00-21:00 UTC. Direct trace readback added correlated traces missed by search. Native 22 spans, cf2otel 25 spans, 21 strict pairs matched by end time within 2 s, model suffix and exact input/output token counts; all eight native attribute keys present on all 21. Cutover count gate passes. P1 HTTP ray-ID duplicate proof remains unproven because observed rows carry no ray ID, and P2 exporter-failure replay is pending; therefore AC2/3 remain parked and no Cloudflare write occurred.

Wave 2 final cutover gate: P2 full-exporter-outage replay passed at checkpoint 22:14:29Z with one source ID, one Loki request row and one exact Tempo span; P7 has 21 strict native/cf2otel pairs. P1 AI Gateway restart proof passed 16/16, but the required HTTP duplicate-ray check has no input because all 50 observed HTTP rows lack populated cloudflare.http.ray_id. Thus P1 is partial, the P8 gate is not met, and no gateway PUT was attempted. Fresh read-only gateway GET still found one native OTel export entry. Resume after a restart window with populated HTTP ray IDs proves no duplicates; then recapture gateway and Workers destination pre-states before a single scoped cutover.

Loop 3 P1h passed the composite-key HTTP restart proof and released cutover. Fresh Gateway pre-state and Workers destinations were captured privately. The scoped request removing the sole native OTel entry returned HTTP 400; the prepared rollback request also returned HTTP 400. Fresh GET exactly matched pre-state with one native entry, so no Gateway configuration changed; Workers destinations remained the same apart from independent job-status timestamps. AC2/3 stay parked. Resume requires a newly authorized corrected request body after inspecting the captured error response, with fresh pre-state and destination readback. No further PUT was sent under this run budget.

Loop 4 final-runtime gate passed on [14:43,15:43) UTC after aigateway.logs checkpoint 15:45:23Z: 69 source IDs, 69 exact Loki request rows, 69 direct Tempo spans, one per ID. One body-unavailable request lacked a content-log trace ID; exact-ID Tempo search located its trace and direct readback confirmed the span. Fresh gateway pre-state had one native OTel destination; two Workers observability destinations were saved privately. A 23-field no-op body retaining three null fields succeeded (2xx; exact code not retained), and re-GET changed only modified_at. The one cutover PUT returned HTTP 200; re-GET changed only otel to [] and modified_at. The Workers destination objects then differed only in configuration.jobStatus.last_complete on both destinations; their stable configuration was identical. The private script incorrectly treated those autonomous timestamps as a configuration change and sent the one authorized rollback PUT, HTTP 200. Final gateway re-GET restored the sole native OTel entry and all pre-state fields apart from modified_at; Workers stable configuration remained identical. The private comparator is corrected and a local regression check distinguishes timestamp drift from a real configuration change. No post-cutover watch was run because rollback restored native export. AC2/3 remain open; the three authorized gateway writes are exhausted. Resume requires a fresh scoped cutover authorization and live gate, using the corrected destination comparison.

Loop 5 P8-T regression passed; P8.0 [2026-09-25T01:37:48Z,04:37:48Z) had 0 source IDs after the aigateway.logs checkpoint passed its end. No Cloudflare PUT was sent (0/3 authorized writes used); closeout GET still had one native OTel entry and two Workers destinations. Park AC2/3 until a fresh 60-minute window has at least three source IDs, extending once to three hours, and exact Loki/Tempo one-to-one proof passes. Implementation attempts: tooling 1; review-repair 0; infrastructure retries 0; grant: 2026-09-25 three-write decision.

Loop 5 owner closeout: P8.0 three-hour exactness window had zero source IDs, zero Loki rows and zero exact Tempo spans. P8 no-op, cutover and conditional rollback were not sent (0/3 authorized writes used); native AI Gateway OTel export remains configured and Workers destinations remain present. Resume only after a fresh 60-minute running-binary window has at least three source IDs with one-to-one Loki and direct Tempo proof, allowing the defined extension to three hours. Tooling implementation 1/4, review-repair 0/3, infrastructure retries 0; grant: frozen three-write P8 decision.
<!-- SECTION:NOTES:END -->
