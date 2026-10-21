---
id: CFO-0086
title: >-
  AI Gateway: take token usage from the response body when the log record
  reports zero
status: To Do
assignee: []
created_date: '2026-10-21 01:10'
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
- [ ] #1 When the request record's usage is zero or missing and the response body parses with result.usage or usage, the request log, GenAI span and token metrics carry the body's input and output tokens
- [ ] #2 An attribute marks body-derived usage (for example cloudflare.ai_gateway.usage_source=response_body) so it stays distinguishable from gateway-reported usage
- [ ] #3 Tests cover the Workers AI result.usage shape, the OpenAI-style top-level usage shape, a truncated body and a non-JSON body
- [ ] #4 After deploy, daily input tokens for jev in Grafana match the Cloudflare dashboard to within 2%
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
