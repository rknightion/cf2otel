---
id: CFO-0065
title: Harden raw Markdown release-note coverage or reject that input explicitly
status: In Progress
assignee:
  - '@loop15-root'
created_date: '2026-10-01 07:32'
updated_date: '2026-10-01 19:13'
labels:
  - release
dependencies: []
priority: medium
type: bug
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 preserved an unshipped scratch parser after independent review proved false passes: angle-bracket link destinations containing parentheses and nested label brackets exposed hidden titles as release coverage. The operational gate now feeds the unchanged checker official GitHub rendered PR body text, which correctly includes linked issue references and excludes hidden destinations. Scratch commits remain local on loop14/relnotes-park; none of that parser was published.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The supported release-note input contract is explicit about rendered text versus raw Markdown
- [ ] #2 Hidden Markdown destination and title text cannot satisfy required visible release-note descriptions for any supported input
- [ ] #3 Rendered issue references match without waiving required subject, distinct-entry or patch-equivalence checks
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop15 E65: rendered-text-only CLI contract; reject raw Markdown explicitly rather than extending the parked parser; assertion-based public CLI regression for hidden destinations and rendered issue references, retaining subject/distinct-entry/patch-equivalence checks; full local gate and CodeRabbit.
<!-- SECTION:PLAN:END -->
