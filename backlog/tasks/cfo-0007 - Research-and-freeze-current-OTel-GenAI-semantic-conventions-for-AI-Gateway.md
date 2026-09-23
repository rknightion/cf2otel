---
id: CFO-0007
title: Research and freeze current OTel GenAI semantic conventions for AI Gateway
status: In Progress
assignee:
  - '@codex'
created_date: '2026-09-23 10:04'
updated_date: '2026-09-23 11:40'
labels:
  - 'wave:1'
  - aigw
  - genai
dependencies: []
priority: high
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Use the otel-semantic-conventions skill and live upstream sources (not memory): GenAI conventions change often. Output is a frozen mapping from every AI Gateway log field to gen_ai.* span, event, log and metric names, plus cloudflare.ai_gateway.* for the rest.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A committed spec (spec/genai-mapping.md or similar) names the semconv version/commit it was taken from
- [ ] #2 Every AI Gateway REST log field in doc-0003 has a disposition: gen_ai.* attribute, cloudflare.* attribute, metric, event, or deliberately dropped with a reason
- [ ] #3 Content capture follows the current GenAI guidance for opt-in message content
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Pin current upstream GenAI semantic conventions to an exact commit, map every doc-0003 AI Gateway field and native span attribute, then commit the reviewed mapping after W0.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
W1 general subagent returned spec/genai-mapping.md. Field inventory: 48 Cloudflare fields and all 8 native attributes mapped; upstream commit 8ffdf568e1b4391a99adb081db16e8102e36918e. Pending commit and W7 implementation validation.
<!-- SECTION:NOTES:END -->
