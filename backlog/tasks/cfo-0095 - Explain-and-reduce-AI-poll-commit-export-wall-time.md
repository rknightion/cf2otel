---
id: CFO-0095
title: Explain and reduce AI poll commit/export wall time
status: Done
assignee:
  - '@codex'
created_date: '2026-11-06T00:40:00Z'
updated_date: '2026-11-06T09:37:00Z'
labels: []
dependencies: []
priority: medium
ordinal: 125000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Release0.20.1 live completed-poll instrumentation exposed commit/export work dominating the largest observed AI poll:132 committed source records,4 windows,274.630s total,98.937s collect,1.659s commit-lock wait and174.034s commit. Another64-record poll spent71.193s committing. Collection and checkpoints succeed, but export cost consumes much of a5-minute polling budget even after request amplification and fetch concurrency fixes. This is a measured performance follow-up, not an observed data-loss failure. Preserve flush-gated checkpoint and replay semantics; do not loosen alert thresholds or sacrifice source delivery for speed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A bounded reproduction or fenced live measurement identifies which operations account for commit wall time, separating export, replay and checkpoint work where necessary
- [x] #2 Any optimization preserves delivery acknowledgement, replay behavior, bounded buffering and checkpoint ordering with meaningful regression coverage
- [x] #3 Before/after measurements on comparable record and payload volumes establish the effect, or document evidence that the cost is external and the appropriate operational action
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Attribute commit wall time through scheduler/exporter inspection and a bounded reproduction. Optimize only the measured cost while retaining flush acknowledgement, replay and checkpoint ordering. Compare identical payload workloads, run focused failures and required gate, obtain combined review, then publish and verify the live change if supported.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Published177a147: each bounded source commit chunk now flushes logs and traces concurrently, joins both acknowledgements and then checks the unchanged export-failure observer before scheduler metric replay/checkpoint persistence. The bounded real-HTTP reproduction identified serial independent signal flush waiting as avoidable commit time; it failed before the change because only logs reached the receiver. Success, logs503, traces503 and cancellation cases pass, as do existing checkpoint recovery, partial failure, bounded export and concurrent isolation race tests. Same workload benchmark(5 iterations,one16KiB log plus one span,25ms receiver delay per signal):51.288ms/op before,25.926ms/op after,49.5% reduction in this controlled flush workload. Synchronous full log batches during replay remain outside this overlap; no claim of49.5% total live poll improvement. Full isolated just check PASS; remote gate hosts unreachable, main checkout scratch Go files excluded via exact candidate clone. Advisory review clean. Combined Kodus and independent agent review PASS with zero findings, matching candidate tree2686f78c12375150a82eabdc82f0869109b7f3d0; evidence /tmp/cf2otel-cfo95-review/combined.json and adjacent provider artifacts. No new signals, buffer changes or packaging changes; other DoD items inapplicable. Source pushed; hosted CI and live rollout not yet verified.
<!-- SECTION:FINAL_SUMMARY:END -->
