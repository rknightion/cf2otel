---
id: CFO-0016
title: 'Compare against native AI Gateway OTel export, then disable it'
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 23:01'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC2/3: 62 matched native/cf2otel requests carried all eight native attribute keys; model suffix and numeric cost agree. The native gateway still has one OTel export entry. Cutover was withheld because prior partial AI Gateway windows produced duplicates and W16 no-duplicate acceptance failed. No Cloudflare write was made; fix delivery atomicity and reverify before disabling.

Wave 2 P7 at v0.2.3, 2026-09-23 17:00-19:15 UTC: native 17 spans, cf2otel 14, 14 strict pairs with all eight native keys present. New-cutover gate needs 20 pairs plus P1/P2; P1 currently has only 13/16 exact-ID Tempo spans. Gateway OTel export remains enabled and no Cloudflare write was made.

Correction: the P1 three-span gap was a Tempo TraceQL search false negative. Direct trace readback finds 16/16 exact source-ID spans. The P7 search table still has only 14 strict pairs and the P2 gate is pending, so cutover remains withheld.

P7 direct-trace correction: Tempo search missed four correlated cf2otel traces. Adding their exact Loki content-log trace IDs and verifying each through direct Tempo trace readback gives 17 native, 18 cf2otel spans and 17 strict pairs in 17:00-19:15 UTC, with all eight native keys present on all 17 pairs. Still below the 20-pair cutover threshold; no gateway write.

Wave 2 expanded P7 on v0.2.3 after checkpoint 21:14:29Z: Tempo native query {resource.service.name="ai-gateway" && name="cf.aig.request"} and cf2otel query {resource.service.name="cf2otel" && span.gen_ai.operation.name="chat"}, 2026-09-23 17:00-21:00 UTC. Direct trace readback added correlated traces missed by search. Native 22 spans, cf2otel 25 spans, 21 strict pairs matched by end time within 2 s, model suffix and exact input/output token counts; all eight native attribute keys present on all 21. Cutover count gate passes. P1 HTTP ray-ID duplicate proof remains unproven because observed rows carry no ray ID, and P2 exporter-failure replay is pending; therefore AC2/3 remain parked and no Cloudflare write occurred.

Wave 2 final cutover gate: P2 full-exporter-outage replay passed at checkpoint 22:14:29Z with one source ID, one Loki request row and one exact Tempo span; P7 has 21 strict native/cf2otel pairs. P1 AI Gateway restart proof passed 16/16, but the required HTTP duplicate-ray check has no input because all 50 observed HTTP rows lack populated cloudflare.http.ray_id. Thus P1 is partial, the P8 gate is not met, and no gateway PUT was attempted. Fresh read-only gateway GET still found one native OTel export entry. Resume after a restart window with populated HTTP ray IDs proves no duplicates; then recapture gateway and Workers destination pre-states before a single scoped cutover.
<!-- SECTION:NOTES:END -->
