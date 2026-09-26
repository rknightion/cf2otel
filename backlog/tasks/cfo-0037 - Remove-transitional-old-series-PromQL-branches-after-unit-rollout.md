---
id: CFO-0037
title: Remove transitional old-series PromQL branches after unit rollout
status: To Do
assignee: []
created_date: '2026-09-26 11:49'
updated_date: '2026-09-26 11:49'
labels: []
dependencies:
  - CFO-0029
references:
  - codex/l29-name-map-loop7.md
priority: low
type: chore
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
CFO-0029 shipped transitional new-or-old PromQL expressions so dashboards and alerts survived the metric-unit rename. Loop 7 deployed v0.6.0 and observed new seconds and ratio families in m7kni Mimir. The old branches now keep legacy series in query paths and should be retired in a focused Grafana change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Dashboard and alert expressions for renamed series use only the observed new metric names and preserve the same aggregation and thresholds
- [ ] #2 just gen-check and just check pass, and grafana-sync read-back shows the updated dashboard and alert rule expressions on m7kni
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Observed v0.6.0 new-name families: gen_ai_client_operation_duration_seconds, cf2otel_api_duration_seconds, cf2otel_scrape_duration_seconds, cf2otel_window_gap_seconds_total, cf2otel_scrape_last_success_timestamp_seconds, cf2otel_checkpoint_age_seconds, cloudflare_http_origin_duration_seconds, cloudflare_rum_lcp/inp/fid/fcp/ttfb_p75_seconds, cloudflare_access_apps/users_ratio, cloudflare_rum_cls_p75_ratio, cf2otel_build_info_ratio. Histogram bucket/sum/count suffixes follow the duration rename. The root map in codex is machine-local; this note keeps the names in the public task.
<!-- SECTION:NOTES:END -->
