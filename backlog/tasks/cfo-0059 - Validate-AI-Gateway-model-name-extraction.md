---
id: CFO-0059
title: Validate AI Gateway model name extraction
status: Done
assignee: []
created_date: '2026-09-30 21:44'
updated_date: '2026-10-01 07:32'
labels:
  - aigateway
dependencies: []
priority: low
type: bug
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Some gen_ai.request.model values are empty after the provider prefix or are not model names at all, which splits the per-model cost and token tables.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Model and provider parsing is covered by table tests for prefixed, unprefixed and malformed inputs
- [x] #2 Unparseable values are mapped to a documented placeholder rather than an empty or arbitrary string
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Normalized model identifiers consistently across request/content logs, spans and metrics; malformed values map to documented unknown. Candidate 6eb977749dc18fab947163e37bfe7468308683c3 has assertion-red public Register reproduction, passing package/full gates and complete four-file CodeRabbit review with zero findings. Root landing 32fa88ffc62e47e362756a25506baedb4c9513b5 passed gate/scan and was pushed. Live deployment not claimed; no Dockerfile or packaging change.
<!-- SECTION:FINAL_SUMMARY:END -->
