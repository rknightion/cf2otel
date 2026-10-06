---
id: CFO-0082
title: Upgrade OpenTelemetry Go to the v1.47 line
status: Parked
assignee: []
created_date: '2026-10-06 10:02'
updated_date: '2026-10-06 11:44'
labels:
  - deps
  - telemetry
dependencies: []
priority: medium
type: task
ordinal: 111000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
PR38 failed on the OpenTelemetry upgrade. Track a compatible release-set upgrade with API adaptation and exact-source validation rather than pinning back incompatible log APIs. Root owns post-auto-RC telemetry proof and finalization.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Read the PR38 failing log and upstream changelog through Firecrawl before implementation; record the compatibility evidence.
- [x] #2 All go.opentelemetry.io/otel* modules move to the v1.47 release set: otel, metric, trace, sdk, sdk/metric and OTLP metric/trace exporters v1.47.0; otlplog exporters v0.23.0; prometheus v0.69.0; log and sdk/log use the versions required by otlplog v0.23.0, with API adaptation, not pinback.
- [x] #3 No other version changes except named MVS requirements; go mod tidy is clean; internal/semconv diff is empty; no tests are weakened.
- [x] #4 just check and exact-SHA ci-success are green.
- [ ] #5 Root verifies post-auto-RC telemetry build_commit matches the accepted SHA, every collector last-success age is <1800s within 30 minutes, failed-attempt delta is zero over the observation window, and fresh Loki logs are present. Long-interval collectors may be reread once at 45 minutes. Root owns telemetry proof and finalization.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
OT lane: read PR38 job and PR37 module diff plus upstream release/API docs; upgrade the complete OTel release set using exporter module requirements; adapt only proven API incompatibilities while preserving the wire contract; tidy, exercise local telemetry export, run just check and CodeRabbit, then return uncommitted for root security review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Compatibility evidence before implementation: PR38 build/vet/test run 37127010446 job 111520175636 compiled and passed race tests; it failed at go mod tidy -diff because go.sum retained old sums and metric/x needed v0.69.0. PR38 mixed v1.47.0 API/SDK with previous exporters; PR37 lists the full release set. Firecrawl read https://github.com/open-telemetry/opentelemetry-go/releases/tag/v1.47.0 and https://pkg.go.dev/go.opentelemetry.io/otel/log@v1.47.0. Logs API/SDK stabilized at v1.47.0; both otlplog v0.23.0 go.mod files require log and sdk/log v1.47.0, not v0.23.0. Current call sites already use attribute.Value/attribute.KeyValue as required by the stable API, so no call-site adaptation or pinback is justified. prometheus v0.69.0 requires procfs v0.22.0; this is the sole non-OTel MVS version increase expected.

Implemented the full OTel release set, including log/sdk/log v1.47.0, indirect otlptrace v1.47.0, metric/x v0.69.0 and upstream-test-only sdk/log/logtest v0.23.0. Sole non-OTel MVS increase is github.com/prometheus/procfs v0.21.1 -> v0.22.0, required by prometheus v0.69.0; selected-module before/after comparison confirms no other changes. go mod tidy completed; cmd/ and internal/ diffs, including semconv and all tests, are empty. Existing local race-enabled telemetry boundary tests passed uncached: OTLP HTTP metrics/logs/traces decoded at an httptest receiver, log bodies/attributes and span context, denied/inferred identity cases, Prometheus alongside OTLP, and rejected-datapoint handling. No production call-site changes were needed because baseline already uses the stable attribute APIs. Required-version check failed on unchanged base and passed after upgrade. Root must still review, commit/push, observe exact-SHA ci-success and post-auto-RC telemetry; criteria/status left untouched.

Loop26: 1 implementation attempt; upgrade landed at 97698a0de490d3b42dd6d545ce42cac84cca1f14 with local/composed just check and exact-SHA CI run 37451033089 ci-success green. Security review and CodeRabbit complete, zero findings. auto-RC run 37452249712 green. Post-RC conservative epoch 2026-10-06T10:57:10Z: exact build, 49 collector census, single current instance, zero resets and fresh Loki verified. At the sole authorized 45-minute reread, certs.packs, httpreq.threats and httpreq.transfer had zero attempts and ages 2798.461s, 2838.461s and 2828.461s. Their source defaults are hourly, so absence of an attempt is not an observed failure; all-collector age acceptance remains unmet. Raw attempt-minus-success counters and sampled window extrema are zero for all 49; tiny PromQL extrapolation differences are not integer failed attempts. Parked needs owner: approve interval-aware acceptance or additional bounded observation; interval changes and live-hour proof were excluded. Resume telemetry acceptance only, not implementation. AC5 deliberately unchecked.
<!-- SECTION:NOTES:END -->
