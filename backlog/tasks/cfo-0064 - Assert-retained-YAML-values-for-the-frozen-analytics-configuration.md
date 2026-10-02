---
id: CFO-0064
title: Assert retained YAML values for the frozen analytics configuration
status: Done
assignee: []
created_date: '2026-10-01 07:32'
updated_date: '2026-10-02 06:26'
labels:
  - config
dependencies: []
priority: low
type: task
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 seam review passed the configuration contract but identified a regression-coverage gap: valid YAML cases currently assert successful validation without checking the retained zero cardinality, all request-source policy and explicit empty breakdown list. Environment cases assert values directly; a YAML-specific ignored-value regression could pass existing tests.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Existing YAML integration cases assert retained zero cardinality, all request source and empty breakdown values
- [x] #2 A deliberate ignored-value mutation makes the relevant assertions fail without relying on a compile error
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Test-only admission after core writer completion and prior core publication park. Extend existing YAML integration cases with retained zero-cardinality, all-request-source and explicit-empty-breakdown assertions. Prove a deliberately ignored YAML-value mutation fails those assertions for the correct reason in isolated scratch; restore production code unchanged. No production config, source defaults or policy changes. Source base d08; E51F review is read-only in separate scratch, so config test ownership no longer overlaps an active source writer.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Extended existing YAML integration cases to assert retained zero-cardinality, all-request-source and empty-breakdown values. Three real-loader ignored-value scratch mutations passed the old tests and failed only the corresponding new assertion; restored production code is byte-identical to base. Owned 14-line test patch passed full gate and complete CodeRabbit review. Published via edbefed/a77ce0b; exact final gate, literal-zero net/history scans and applicable CI workflows/jobs succeeded. One native OpenAI-404 infrastructure retry, no implementation reset or repeated passed gates. No production behavior change or live-runtime claim.
<!-- SECTION:FINAL_SUMMARY:END -->
