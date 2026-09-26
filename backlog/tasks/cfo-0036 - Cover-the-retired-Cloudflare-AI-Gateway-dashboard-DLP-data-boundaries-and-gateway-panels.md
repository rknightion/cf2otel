---
id: CFO-0036
title: >-
  Cover the retired Cloudflare AI Gateway dashboard: DLP, data boundaries and
  gateway panels
status: To Do
assignee: []
created_date: '2026-09-26 09:24'
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
- [ ] #2 cf2otel captures the gateway metadata 'data boundaries' / exception fields the old dashboard read, or documents why the API no longer exposes them
- [ ] #3 The cf2otel dashboard's AI Gateway row adds cache-hit ratio, rate-limited (429) count, cost by provider, a per-model usage table, failed requests and DLP-flagged requests panels
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
