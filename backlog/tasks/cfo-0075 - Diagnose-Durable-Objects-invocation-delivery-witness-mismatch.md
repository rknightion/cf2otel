---
id: CFO-0075
title: Diagnose Durable Objects invocation delivery witness mismatch
status: To Do
assignee: []
created_date: '2026-10-02 16:01'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop16 HOUR-0 on the unchanged running release ended after eight reads because durableobjects.invocations had seven attempts but six successes over the observed span. This is a delivery-witness mismatch, not yet an explained API defect; the prepared hour helper is unchanged. The single authorised fresh-hour retry is independent evidence, not a repair.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The attempt-success mismatch is attributed to a collector failure or metric-observation coherence with exact timestamped, sanitised evidence.
- [ ] #2 Any required source correction has an assertion-failing reproduction and candidate pass, without weakening the hour-proof contract.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
