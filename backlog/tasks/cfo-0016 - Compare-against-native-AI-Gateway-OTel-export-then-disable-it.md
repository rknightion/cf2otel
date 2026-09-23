---
id: CFO-0016
title: 'Compare against native AI Gateway OTel export, then disable it'
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 21:21'
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
<!-- SECTION:NOTES:END -->
