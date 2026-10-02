---
id: CFO-0070
title: Verify OTLP HTTP partial-success rejection reporting
status: In Progress
assignee:
  - '@loop16-root'
created_date: '2026-10-02 11:24'
updated_date: '2026-10-02 13:52'
labels: []
dependencies: []
priority: high
type: spike
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop15 source review of the pinned OpenTelemetry Go HTTP metrics exporter v1.46.0 found a potential partial-success reporting gap: errors.Join evaluates uploadErr before a request callback assigns rejected-datapoint errors to that variable. This was inspected in cached source, not reproduced against a fake endpoint, and no live rejection is asserted. Application export-success markers therefore cannot currently establish complete acceptance for a missing failure-counter proof. The existing atomic-window delivery task covers transport failure and flush gating, not this SDK partial-acceptance boundary. No dependency change or deployment is authorized by this capture.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An offline reproduction against the pinned SDK establishes whether a valid OTLP partial-success response with rejected metric datapoints is reported as an error, with a fully accepted response as control; any new check is observed failing for the intended reason before a correction
- [ ] #2 The verified SDK behavior and its consequence for cf2otel export-success and runtime absence-proof claims are documented without treating HTTP 200 or missing diagnostic series as complete acceptance
- [ ] #3 If a defect is confirmed, the proposed upstream fix or dependency change identifies the exact corrected contract and local validation needed before a separately authorized rollout; otherwise the non-defect conclusion includes the reproduction evidence
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop 16 S70 drives the pinned real exporter through cf2otel using full-acceptance and partial-success HTTP responses, records the observed reporting contract, and documents the consequence without changing dependencies.
<!-- SECTION:PLAN:END -->
