---
id: CFO-0008
title: 'AI Gateway log collector: logs, GenAI traces and metrics'
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 21:19'
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
- [x] #4 Cursor persists across restarts with no duplicate spans; the initial backfill window is configurable
- [x] #5 Verified live: spans for a real gateway request appear in Tempo on the m7kni stack under service.name=cf2otel with a strict superset of the native span's attributes
- [x] #6 When body capture is enabled, each request's captured content (capped at ai_gateway.max_body_bytes, truncation flag true exactly when cf2otel's own cap truncated it) is emitted on the span as today AND as a correlated gen_ai.client.inference.operation.details OTLP log record carrying the span's trace and span IDs; live, a request whose content exceeds 2 KiB appears intact in Loki
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: emit correlated capped content logs and prove no duplicate spans after restart.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parked AC4: live v0.1.3 produced 62 matched native/cf2otel spans with all eight native attribute keys on those matches, but failed pre-checkpoint window attempts emitted duplicates. Loki had 222 AI Gateway rows for 69 unique log IDs, 153 duplicates, max six copies. Resume with atomic window delivery or dedupe and a fresh restart/failure proof.

Finding 2026-09-23 (wave 2 prep): Grafana Cloud Tempo on the m7kni stack stores gen_ai.input.messages / gen_ai.output.messages truncated at exactly 2048 characters (8 spans checked, v0.1.2 and v0.1.3; no SDK attribute limit or OTEL_ env is set in cf2otel or on camden, so the truncation is Tempo-side). The wave 1 comparison proved key presence, not content fidelity. Decision 2026-09-23 (Rob): keep full capped bodies on spans (the Tempo limit may be raised by Rob; out of wave scope) and also emit them as a correlated log record so full content is retained in Loki.

Wave 2 P3 at v0.2.3: 2026-09-23 18:08:34-19:08:35 UTC, 16 source log IDs, 32 source bodies, 32 correlated Loki content rows, all 32 body byte lengths equal content.length metadata; selected 138050-byte body matches its Tempo trace/span IDs; largest 138145-byte source body is represented by a 138050-byte parsed content log under the 163840-byte cap. Tempo query was widened for delayed ingestion, with exact log-ID matching. just check/ci passed in cf2otel-verify at 8e66817, and semconv/docs inventory updated. AC4 remains open: P1 has 16/16 single Loki rows but only 13/16 Tempo spans.

Correction to prior P1 note: Tempo TraceQL search returned only 13/16 for the restart window, but each of all 16 source IDs had exactly one correlated content-log trace ID, and direct GET /tempo/api/traces/{traceID} returned exactly one span whose cloudflare.ai_gateway.log.id equals that source ID. The remaining three spans were present; search had false negatives. Loki remained exactly 16/16 with no duplicate IDs. AC4 now proven for AI Gateway restart and configurable initial backfill.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Wave 2 added correlated capped content OTLP logs while retaining span attributes. Live v0.2.3 proof found 16/16 source IDs once in Loki and once by direct Tempo trace readback after restart, plus 32/32 content bodies in Loki with a 138050-byte trace-correlated example. Focused tests, just check, just ci and review passed.
<!-- SECTION:FINAL_SUMMARY:END -->
