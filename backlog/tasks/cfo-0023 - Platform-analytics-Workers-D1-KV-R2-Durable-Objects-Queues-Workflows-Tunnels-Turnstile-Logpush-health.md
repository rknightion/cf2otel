---
id: CFO-0023
title: >-
  Platform analytics: Workers, D1, KV, R2, Durable Objects, Queues, Workflows,
  Tunnels, Turnstile, Logpush health
status: Done
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-25 14:54'
labels:
  - 'wave:2'
  - platform
dependencies: []
priority: low
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Enabled account datasets from doc-0003 section 2. Avoid duplicating what the native Workers OTLP export already sends.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A per-dataset decision (build/skip, with reason) is recorded before building
- [x] #2 Collectors for the 22 build datasets in AC1's table emit only the frozen semconv names and bounded attributes, with tests, docs/signals.md and docs/configuration.md rows
- [x] #3 A release containing them is deployed to camden and healthy, with every pre-existing checkpoint monotonic and the new checkpoints present
- [x] #4 Each built dataset with source rows in a post-deploy proof window (up to 3 h) matches its m7kni metric within the frozen tolerance; a dataset without source rows is recorded as absent input only if the same source aggregate over the prior 7 days returns rows
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 5: freeze semconv/config/registration seam from SEAM-D, implement five disjoint collector family lanes, integrate docs and dashboard, review, release, deploy to Camden, then compare each built dataset with live source aggregates and m7kni metrics.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
R-23 read-only decision sample: 2026-09-17T13:41:03Z to 2026-09-24T13:41:03Z UTC. Every queried dataset advertised enabled=true. Each query selected one advertised field, below maxNumberOfFields; limit=2. Rows=2 means at least two, not a total.

| Family | Dataset | Sample rows | Fields/max | Native Workers OTLP overlap | Decision and reason |
|---|---|---:|---:|---|---|
| Workers | `workersInvocationsAdaptive` | 2+ | 1/35 | invocation logs/traces | Skip raw events: native Workers OTLP already exports invocation logs and traces. |
| Workers | `workersOverviewRequestsAdaptiveGroups` | 1 | 1/30 | no aggregate-metric overlap | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| D1 | `d1AnalyticsAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| D1 | `d1QueriesAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| D1 | `d1StorageAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| KV | `kvOperationsAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| KV | `kvStorageAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2BandwidthUsageAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2CatalogDataOperationsAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2CatalogTableMaintenanceAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2OperationsAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2StorageAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| R2 | `r2sqlOperationsAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Durable Objects | `durableObjectsInvocationsAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Durable Objects | `durableObjectsPeriodicGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Durable Objects | `durableObjectsSqlStorageGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Durable Objects | `durableObjectsStorageGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Durable Objects | `durableObjectsSubrequestsAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Queues | `queueBacklogAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Queues | `queueConsumerMetricsAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Queues | `queueDelayedBacklogAdaptiveGroups` | 2+ | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Queues | `queueMessageOperationsAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Workflows | `workflowsAdaptive` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Workflows | `workflowsAdaptiveGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Workflows | `workflowsBlockedReasonAdaptiveGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Workflows | `workflowsQueueDepthAdaptiveGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Workflows | `workflowsQueueWaitAdaptiveGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Tunnels | `cloudflareTunnelsAnalyticsAdaptiveGroups` | 0 | 1/30 | none for this family | Skip now: no source rows in the seven-day sample; reopen on natural traffic. |
| Turnstile | `turnstileAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |
| Logpush health | `logpushHealthAdaptiveGroups` | 1 | 1/30 | none for this family | Build bounded aggregate metrics in a later loop; source rows exist and native Workers OTLP does not cover this family. |

Loop 5 SEAM-W pushed at c2a386c7090ac5eb5b5a70b47683b89c5a620f29 after just check and CodeRabbit. The one CodeRabbit major is the explicitly temporary no-op registration stubs; require zero stubs before release. Root judgement: queue gauges name the maximum queue value of each source avg field.

Loop 5 AC2 source and docs shipped at 182b285d6b24e2d683ac837c5ac324d312a4dcc1: 22 Groups collectors, 33 declared platform names, five collapsed dashboard rows, exact-SHA just check and just ci exit 0, CI ci-success and all required workflows success. F-lane implementation attempts F1=2, F2=2, F3=1, F4=1, F5=1; root corrections handled bucket splits, checkpoint hold and lint. CodeRabbit found and repaired a material R2 split defect; final dashboard review's JSON regeneration claim was disproven by gen-check and live m7kni readback. REV-23 independent Sol/medium child could not be dispatched on the current agent control surface, so release PR #18 stays open; AC3/4 unproven. Review-repair budget remains 3/3 for task-level REV; infrastructure retries included CodeRabbit rate limits and one WebSocket close, no extra Cloudflare write. Grant: frozen CFO-0023 build/release/deploy decision. Resume at REV-23 on exact pushed 182b285, then REL/DEP/LIVE.

Loop 5 closeout: v0.5.1 release and Camden deployment healthy; all 36 prepatch checkpoints advanced, none missing or rewound. Natural source-to-Mimir comparisons matched 16 counter metrics, Turnstile count 6, four Queue gauges on source-bearing buckets, and six storage gauges after the max-field patch. R2 SQL and Durable Objects request-body Groups had zero postdeploy rows through 10:15Z but each had rows in the same prior-seven-day aggregate: absent input, not delivery proof. The source watcher was stopped at owner closeout. Implementation attempts F1=2/4, F2=2/4, F3=1/4, F4=1/4, F5=1/4; review-repair platform 2/3 and max-field FIX 1/3; infrastructure retries CodeRabbit rate limit and WebSocket close; grant: frozen build, release and deploy decision.

Loop 6 opportunistic closeout, 2026-09-25T14:53Z: durableObjectsSubrequestsAdaptiveGroups returned one complete 12:00Z bucket with sum.requestBodySizeUncached=21685 over [10:15Z,14:53Z). Camden durableobjects.subrequests checkpoint reached 14:35Z. Mimir query sum(cloudflare_durableobjects_subrequests_request_body_bytes_total{service_name="cf2otel"}) had no series at 11:59Z or 12:10Z, then one series valued 21685 at 12:20Z and 14:53Z. This is an exact natural-source delivery observation for the formerly sparse request-body metric. r2sqlOperationsAdaptiveGroups returned zero rows in the same source interval, so R2 SQL delivery remains unproven absent input.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Loop 5 shipped 22 platform Groups collectors and dashboard panels in v0.5.1. Camden is healthy with monotonic checkpoints. Every dataset with observed postdeploy source rows matched m7kni; two sparse datasets are recorded only as absent input with prior-seven-day source rows.

Loop 6 later observed Durable Objects request-body delivery: one 12:00Z source bucket of 21685 bytes matched the newly appearing Mimir series at 21685 after checkpoint delivery. R2 SQL still had no source rows through 14:53Z and remains unproven.
<!-- SECTION:FINAL_SUMMARY:END -->
