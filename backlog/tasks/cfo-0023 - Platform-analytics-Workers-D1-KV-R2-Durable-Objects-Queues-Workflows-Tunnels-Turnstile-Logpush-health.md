---
id: CFO-0023
title: >-
  Platform analytics: Workers, D1, KV, R2, Durable Objects, Queues, Workflows,
  Tunnels, Turnstile, Logpush health
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
updated_date: '2026-09-24 13:49'
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
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

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
<!-- SECTION:NOTES:END -->
