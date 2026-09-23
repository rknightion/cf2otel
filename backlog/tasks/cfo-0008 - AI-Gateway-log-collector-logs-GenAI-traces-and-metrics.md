---
id: CFO-0008
title: 'AI Gateway log collector: logs, GenAI traces and metrics'
status: Parked
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 16:15'
labels:
  - 'wave:1'
  - aigw
  - genai
dependencies: []
priority: high
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
REST /ai-gateway/gateways/{g}/logs. Decision 2026-09-23 (Rob): bodies opt-in, enabled on camden with a size cap; cf2otel becomes the only AI Gateway trace source and must be a strict superset of the native cf.aig.request span (doc-0003 section 4).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Each request produces a GenAI span with timestamps from the log (start = created_at - duration or the documented equivalent), linked to the caller's trace when request_head carries a traceparent
- [x] #2 GenAI metrics (token usage, operation duration, cost, cache hits, errors) follow the frozen mapping
- [x] #3 Bodies are fetched only when enabled, capped at the configured size with truncation flagged, and never logged by cf2otel's own logger
- [ ] #4 Cursor persists across restarts with no duplicate spans; the initial backfill window is configurable
- [x] #5 Verified live: spans for a real gateway request appear in Tempo on the m7kni stack under service.name=cf2otel with a strict superset of the native span's attributes
- [ ] #6 When body capture is enabled, each request's captured content (capped at ai_gateway.max_body_bytes, truncation flag true exactly when cf2otel's own cap truncated it) is emitted on the span as today AND as a correlated gen_ai.client.inference.operation.details OTLP log record carrying the span's trace and span IDs; live, a request whose content exceeds 2 KiB appears intact in Loki
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC4: live v0.1.3 produced 62 matched native/cf2otel spans with all eight native attribute keys on those matches, but failed pre-checkpoint window attempts emitted duplicates. Loki had 222 AI Gateway rows for 69 unique log IDs, 153 duplicates, max six copies. Resume with atomic window delivery or dedupe and a fresh restart/failure proof.

Finding 2026-09-23 (wave 2 prep): Grafana Cloud Tempo on the m7kni stack stores gen_ai.input.messages / gen_ai.output.messages truncated at exactly 2048 characters (8 spans checked, v0.1.2 and v0.1.3; no SDK attribute limit or OTEL_ env is set in cf2otel or on camden, so the truncation is Tempo-side). The wave 1 comparison proved key presence, not content fidelity. Decision 2026-09-23 (Rob): keep full capped bodies on spans (the Tempo limit may be raised by Rob; out of wave scope) and also emit them as a correlated log record so full content is retained in Loki.
<!-- SECTION:NOTES:END -->
