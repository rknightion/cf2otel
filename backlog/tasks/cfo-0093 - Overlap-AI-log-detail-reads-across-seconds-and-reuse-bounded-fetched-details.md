---
id: CFO-0093
title: Overlap AI log detail reads across seconds and reuse bounded fetched details
status: Done
assignee:
  - '@codex'
created_date: '2026-11-05T22:54:00Z'
updated_date: '2026-11-05T23:16:00Z'
labels: []
dependencies: []
priority: high
ordinal: 123000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Budgeted AI log collection restarts prefetch for every source second, serializing sparse traffic. Overflow seconds are fetched again on the next commit. Owner authorized cross-second overlap and bounded reuse while retaining whole-second checkpoint safety.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Sparse records across seconds overlap detail reads with bounded concurrency and retain ordered complete-second delivery
- [x] #2 Successful boundary details are reused across consecutive budgeted calls with explicit memory and lifetime bounds and no gateway or window contamination; failed delivery replays safely refetch
- [x] #3 Cancellation joins workers; failures never advance checkpoints; real-boundary regressions demonstrate reduced requests without lost records
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Reproduce sparse-second serialization and redundant boundary reads with real HTTP tests. Use one ordered three-row prefetch spanning seconds; preserve serial whole-second emission. Retain successful boundary details only with six-entry,16MiB and five-minute bounds keyed by gateway, ID and timestamp; eligibility requires next cursor equal prior mark, and failed-flush replay clears safely. Serialize calls using context-aware guard and join cancellation. Verify eviction, identity, failure and dense-budget contracts, then full gate and review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Sparse overlap and consecutive budget-boundary reuse reproductions now green with unchanged dense-budget checks. Conservative cache accounting benchmark for two2MiB heads plus decoded bodies: about4ms/op,4.26MiB allocated/op,28allocs on local Intel host; bounded network throughput makes this acceptable, cache hits reuse accounting. Count/byte/TTL and cancellation tests in progress.

Independent review identified stale list-row byte accounting on cache hits. Corrected by retaining only gateway/ID/UTC timestamp key; growing re-listed metadata regression failed then passed. Count, byte, TTL, identity, raw backing capacity and cancellation proofs pass; final focused race subset1.705s. Groma maps new cache source into AI Gateway component.

Independent reviewer PASS; focused race rerun1.693s. Advisory review found no new duplication; increased branching is concentrated in explicit cancellation/cache admission and boundary handling. Final composed local gate and dsreview running.

Clarified cache acceptance to match safe final design: reuse between consecutive budgeted calls, not after a failed downstream commit. Replays from the older checkpoint intentionally discard cache. Initial full gate found four import-group formatting issues only, corrected; full gate rerunning.

Full composed gate exposed real shared-quota contention: existing registered workload test completed200 AI logs in210.2s but competing GraphQL only184 requests (<1rps requirement). Do not weaken assertion or raise quota. Investigating bounded fetch pressure/fair admission before landing. Initial gates otherwise passed fmt/lint/vet; full suite still finishing.

Fairness corrected with three-row logical lookahead sharing two physical detail/body requests. No quota changes. Original178 GraphQL/208.6s and cap3 199/214.2s failed unchanged >=1rps criterion; cap2 232/223s passed with200 AI logs committed under300s. Three repeated fairness race runs passed. Overlap test intentionally updated to require exactly2 concurrent physical reads and never more, including fallback bodies; cap3 mutation fails. Final full gate rerunning.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented ordered cross-second lookahead, two physical reads preserving competing GraphQL throughput, and six-entry/16MiB/five-minute boundary reuse. Replays safely refetch. Real-HTTP red/green and mutation proofs, repeated mixed-workload race checks, full isolated just check, independent review and primary/delta dsreview all PASS. No packaging change or new signals in this task. Live recovery remains tracked separately.
<!-- SECTION:FINAL_SUMMARY:END -->
