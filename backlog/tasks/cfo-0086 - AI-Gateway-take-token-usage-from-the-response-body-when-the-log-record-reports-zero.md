---
id: CFO-0086
title: >-
  AI Gateway: take token usage from the response body when the log record
  reports zero
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-21 01:10'
updated_date: '2026-10-21 01:35'
labels:
  - ai-gateway
  - bug
dependencies: []
priority: high
ordinal: 116000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Workers AI custom-model requests through the gateway (jevgrep's `jev` model, path run/typesafe/jev) arrive with usage.total_tokens=0, cost=0 and gen_ai.usage.input_tokens/output_tokens=0 on the cloudflare.ai_gateway.request record. The real usage is only in the response body (result.usage.input_tokens / output_tokens), which we already emit as gen_ai.client.inference.operation.details. On 2026-10-19 this hid 340M of the 355M input tokens shown in the Cloudflare dashboard, so every usage panel and metric built on the request record under-reports by about 90%. Verified with LogQL: sum_over_time of json result.usage.input_tokens on the details events for model jev matches the Cloudflare chart day by day.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 When the request record's usage is zero or missing and the response body parses with result.usage or usage, the request log, GenAI span and token metrics carry the body's input and output tokens
- [x] #2 An attribute marks body-derived usage (for example cloudflare.ai_gateway.usage_source=response_body) so it stays distinguishable from gateway-reported usage
- [x] #3 Tests cover the Workers AI result.usage shape, the OpenAI-style top-level usage shape, a truncated body and a non-JSON body
- [ ] #4 After deploy, daily input tokens for jev in Grafana match the Cloudflare dashboard to within 2%
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce zero/missing gateway usage using captured Workers AI and OpenAI response fixtures. 2. Recover validated nonnegative integer input/output counts from complete untruncated JSON response bodies only when gateway values are zero/missing; preserve positive gateway counts, mark body-derived usage and avoid duplicate attributes. 3. Verify request log, span and metrics consistency, malformed/truncated/non-JSON handling and capture-off behavior; independent review, CodeRabbit delta and remote gate. 4. Deploy and retain the daily dashboard within-2-percent comparison as an explicit live criterion until observed.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reproduction red confirmed missing/zero counts on request log, GenAI span and token metrics for Workers AI result.usage and top-level input/output or prompt/completion usage. Candidate resolves each zero/missing side from complete untruncated JSON only; positive gateway values retained, total recomputed safely when zero/missing, provenance log/span attribute declared centrally and denylist registered. Focused race tests now green, including truncation/non-JSON, invalid/fractional/negative/overflow counts and capture disabled. No live daily-match claim; committed historical rows are not reprocessed. Work remains isolated from the limiter gate candidate.

Independent review found zero usage_metadata could override positive top-level tokens before body fallback. Fixed precedence and added conflicting-source regression; mutation restoring original precedence fails all three signal assertions, corrected fixture passes. Independent delta review PASS. Daily within-2-percent comparison remains open.

Source acceptance 1-3 established by failing then passing integrated log/span/metric fixtures; invalid, truncated and non-JSON cases included, no duplicate attributes or provenance metric dimension. Independent review PASS after precedence correction; CodeRabbit completed zero findings with all token-usage paths covered. Existing GitHub CI on limiter SHA 1661d4b failed govulncheck GO-2026-6809 under Go1.27.2; module floor aligned to already-used Docker Go1.27.3 and final composed gate rerunning on that toolchain. Daily live dashboard criterion remains unchecked.

Final composed remote just check PASS on Go1.27.3: run 20261021T012928Z-cf2otel-2f357034, tree fadea47c2999d5ef3e43d05d986db4d2f84cd712 on 1661d4b; all race suites, lint/vet, tidy, build, vulnerability and generated checks passed. Independent review PASS and complete CodeRabbit zero findings. Publishing recovery source plus Go patch alignment. AC4 remains open: compare a complete post-deploy daily source window once checkpoint lag recovers; no historical replay or cost estimation. No packaging definition change; just ci conditional not applicable.
<!-- SECTION:NOTES:END -->
