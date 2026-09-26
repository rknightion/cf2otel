---
id: CFO-0036
title: >-
  Cover the retired Cloudflare AI Gateway dashboard: DLP, data boundaries and
  gateway panels
status: Parked
assignee: []
created_date: '2026-09-26 09:24'
updated_date: '2026-09-26 13:30'
labels:
  - dashboard
  - ai-gateway
dependencies: []
priority: medium
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Infinity-based 'Cloudflare AI Gateway' dashboard (uid cloudflare-ai-gateway) and its datasource cloudflare-aigw-logs were retired 2026-09-26 in favour of cf2otel. Backup: ~/repos/chat-personal/grafana/backups/coding-agent-dashboards-20260926/cloudflare-ai-gateway.json. cf2otel already collects per-request gateway logs (cached, status, cost, wholesale, provider, model, tokens, latency) and cloudflare_ai_gateway_* metrics, but two things the old dashboard showed are not captured, and several panels have no cf2otel equivalent. Per-request traces (cf.aig.request, rootService ai-gateway) are Cloudflare's own export to Tempo and need nothing here.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 cf2otel captures DLP policy outcomes per gateway request (flagged/blocked, policy id, request vs response) from the gateway logs API, as log attributes, and a flagged-request count metric
- [x] #2 cf2otel captures the gateway metadata 'data boundaries' / exception fields the old dashboard read, or documents why the API no longer exposes them
- [ ] #3 The cf2otel dashboard's AI Gateway row adds cache-hit ratio, rate-limited (429) count, cost by provider, a per-model usage table, failed requests and DLP-flagged requests panels
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop 7 AC2: read-only inspection of the retired dashboard backup found Data boundaries was static provenance text and Gateway metadata exceptions was a row grouping failed requests and DLP, not separate Logs API fields. Fifty current log-detail rows had no boundary or exception keys; docs/signals.md records this distinction and the sampled limit. Landed at c229f18 with just check and CI 36240525924 (ci-success success). AC1 and AC3 remain open: all 50 sample details and three fictional-data requests had null DLP fields, and the SDK does not specify the non-null matched-row JSON shape. Resume collector mapping only with an authoritative matched Logs API schema or a sanitized real matched detail row showing action, direction and policy ID location/cardinality; then independent REV-L36 and dashboard L36D.

Loop 7 run-end park: implementation attempts 0/4, review-repair 0/3, infrastructure retries 0; grant: bounded read-only Gateway probe and six fictional-data requests after Rob enabled Flag DLP. All 50 sampled details and all six exact synthetic log details had null dlp_action/dlp_profiles; five inspected response headers had no cf-aig-dlp, with the first header uninspected. The supplied screenshot proves policy configuration only. AC2 is documented; AC1 requires an authoritative matched Logs API schema or sanitized real matched detail with action, direction, policy ID location/cardinality, then independent REV-L36; AC3 needs L30 and L36 followed by L36D/SYNC-30.
<!-- SECTION:NOTES:END -->
