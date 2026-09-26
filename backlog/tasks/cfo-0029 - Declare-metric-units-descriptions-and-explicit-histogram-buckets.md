---
id: CFO-0029
title: 'Declare metric units, descriptions and explicit histogram buckets'
status: Done
assignee: []
created_date: '2026-09-25 12:07'
updated_date: '2026-09-26 13:30'
labels: []
dependencies: []
priority: medium
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/telemetry/emitter.go creates every instrument with a bare name: no unit, no description and no SDK View, so the seconds histograms (gen_ai.client.operation.duration, cf2otel.api.duration, cf2otel.scrape.duration) use the SDK default buckets (0, 5, 10, 25 ... 10000) and nearly every sample lands in the first bucket. spec/genai-mapping.md froze the GenAI token counters as unit {token}, which is not emitted. Decision (Rob, 2026-09-25): accept the Prometheus series renames that unit suffixes cause in Grafana Cloud, and update dashboards and alerts to the new names.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Units and descriptions are declared in internal/semconv and applied by the emitter; a real-SDK exporter test asserts the unit on one counter, one gauge and one histogram
- [x] #2 gen_ai.client.operation.duration uses the GenAI semconv explicit bucket boundaries and the two cf2otel duration histograms use sub-second boundaries, asserted on exported data points
- [x] #3 docs/signals.md lists every metric's unit, dashboard and alert expressions use the resulting Prometheus names, and just gen-check passes
- [x] #4 After deploy, one renamed series per unit kind is observed on the m7kni stack by exact query, and the old-to-new name map is recorded in the task notes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 6 candidate: declare metric metadata and explicit buckets, apply transitional dashboard expressions, then independently review the seam before landing and prove renamed series after deployment.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Local unpushed candidate defbf92a1fffffe1e451d7b9856bb7c3d245ff8f passed just check and CodeRabbit but independent REV-L29 found double conversion of RUM milliseconds already converted in the collector. Root repair 07b3e5f8075b8ea1b4d3dab1ad25eee08f5c8a84 made the real-SDK test fail first (0.00075 instead of 0.75 seconds), then pass after removing the second scale; just check and focused CodeRabbit passed. Mandatory independent REV-L29-R2 is still absent because the agent dispatch control became unavailable. Nothing from L29 was pushed. Resume with that exact candidate and a fresh independent review of the full e490698..07b3e5f diff; only after PASS reconcile current main, gate, scan and land. AC4 still requires a deployment and exact m7kni Mimir queries. Implementation attempt 1/4 and review-repair 1/3 used; no silent reset.

Loop 7: refreshed candidate faf46e7 passed real-SDK exporter unit and bucket assertions, just check and just gen-check, complete CodeRabbit review and independent REV-L29-R2 PASS. Landed at ac8958c with exact-SHA gate, privacy scan and CI. v0.6.0 deployed healthy on Camden at release merge eb5c2c7; old-to-new Prometheus family map is in private codex/l29-name-map-loop7.md. AC4 awaits exact m7kni Mimir queries after post-deploy delivery.

Loop 7 AC4 at 2026-09-26T11:47:43Z, m7kni Mimir exact queries with service_name=cf2otel: cf2otel_scrape_duration_seconds_count 40 series; cloudflare_r2_storage_payload_bytes 1; gen_ai_client_inference_usage_input_tokens_total 1; cf2otel_api_requests_total 2; cloudflare_rum_cls_p75_ratio 1. Old-to-new families: gen_ai_client_operation_duration -> gen_ai_client_operation_duration_seconds; cf2otel_api_duration -> cf2otel_api_duration_seconds; cf2otel_scrape_duration -> cf2otel_scrape_duration_seconds; cf2otel_window_gap_total -> cf2otel_window_gap_seconds_total; cf2otel_scrape_last_success_timestamp -> cf2otel_scrape_last_success_timestamp_seconds; cf2otel_checkpoint_age -> cf2otel_checkpoint_age_seconds; cloudflare_http_origin_duration -> cloudflare_http_origin_duration_seconds; cloudflare_rum_lcp/inp/fid/fcp/ttfb_p75 -> corresponding _seconds; cloudflare_access_apps/users -> corresponding _ratio; cloudflare_rum_cls_p75 -> cloudflare_rum_cls_p75_ratio; cf2otel_build_info -> cf2otel_build_info_ratio. Histogram bucket/sum/count families follow the duration rename; curly-brace units and counter _total names stay.

Loop 7 run-end: implementation attempts 1/4 carried, no new implementation attempt; review-repair 2/3 total after the bounded PromQL correction; infrastructure retries 0; grant: frozen metric renames and transitional dashboard expressions. Done because exact-SHA tests, independent REV-L29-R2 PASS, healthy v0.6.0 deploy and five unit-kind Mimir series observations proved AC1-4.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Declared metric units, descriptions and duration buckets, preserved old/new Grafana expressions, and verified seconds, bytes, token, request and ratio series on m7kni after the healthy v0.6.0 deploy.
<!-- SECTION:FINAL_SUMMARY:END -->
