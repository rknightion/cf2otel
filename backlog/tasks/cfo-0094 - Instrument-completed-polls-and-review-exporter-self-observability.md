---
id: CFO-0094
title: Instrument completed polls and review exporter self-observability
status: Done
assignee:
  - '@codex'
created_date: '2026-11-05T22:54:00Z'
updated_date: '2026-11-05T23:16:00Z'
labels: []
dependencies: []
priority: high
ordinal: 124000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Aggregate counters cannot reliably attribute source reads, limiter waits, collection and export work to completed polls, especially around restarts. Owner authorized per-poll diagnostics and an OTel-guided review of own-app instrumentation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Completed polls expose accurate request costs and phase timings with success failure and cancellation distinctions
- [x] #2 Instrumentation reuses provider identity, bounded metric dimensions and useful trace-log correlation without duplicate boundaries or sensitive request content
- [x] #3 Local receiver tests prove emitted telemetry and documented signal meanings; version and signal-path review records remaining gaps
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Inspect installed OTel versions and existing provider/signal ownership. Add poll-scoped concurrent request accounting and scheduler phase/committed-count summary. Reuse telemetry providers for operational poll spans and correlated structured logs while preserving independent source trace parentage. Bound metric dimensions and request-role enums. Verify actual emitted successful, failed and canceled poll signals at local OTLP receiver, update signal docs and review remaining gaps.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Discovery: installed OTel core/sdk 1.47.0, OTLP log exporters0.23.0, Prometheus exporter0.69.0. Existing API request duration stops at headers. Existing process log provider is isolated from source commits. Implement operational trace exporter/processor under same Providers owner with same configured destination/resource, isolated from source commit failure accounting. Preserve source trace parentage. Dashboard fixes retired API status-class grouping and clarifies header timing. Exact tagged API evidence recorded by instrumentation lane.

Frozen signal contract: cf2otel.poll.completed structured summary, cf2otel.poll internal operational span, cf2otel.poll.phase.duration seconds by collect/commit_lock/commit, cf2otel.poll.committed.items by windows/logs/spans. Request role list/detail/body/other is bounded; API duration/wait remain aggregate header/wait sums, not additive wall phases. Root added phase and completed-poll dashboard panels and corrected old API grouping.

Receiver proof passes for phase/item metrics, poll/API/log linkage, independent source span parentage and diagnostic400 rejection not contaminating source commit. Mutations removing per-poll counting and reintroducing diagnostic failures into commit accounting fail. Independent review requested source-first shutdown and complete commit-lock/panic timing attribution; corrections in progress before full gate.

Independent review PASS after source-first bounded shutdown, complete mutex-wait attribution and deferred collection timing through panic. Bad-order shutdown and disabled-lock-accounting mutations failed correctly. Tagged OTel v1.47.0 source resolves to66cfc9520e205b7d450183532772401bc2b6674c, retrieved2026-11-05: trace/tracer.go Start creates active span context; sdk/log/logger.go captures context for log correlation. No upgrades or destination changes. Full composed local gate and dsreview running.

Final primary dsreview PASS with zero findings (529.6s). Independent review PASS. Full gate still running; separate small delta review covers physical-read cap correction in AI Gateway. Receiver verification establishes local emission/correlation, not backend receipt. Snapshot collectors have no checkpointed item counts; API header/wait sums overlap; committed logs/spans are telemetry records, not source rows.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added exact poll-scoped request role costs, wall phase histograms, checkpointed item counters and correlated completion logs/operational traces. Separate operational exporter prevents diagnostic failure from vetoing source commits; source-first bounded shutdown, deny policy and source trace parentage preserved. Local OTLP receiver and mutation proofs, full isolated just check, independent review and dsreview PASS. Signal docs and dashboard updated. No dependency/endpoint/packaging changes; live backend verification pending deployment.
<!-- SECTION:FINAL_SUMMARY:END -->
