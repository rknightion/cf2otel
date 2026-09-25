---
id: CFO-0031
title: Document at-least-once delivery and pin a dedupe key per log family
status: To Do
assignee: []
created_date: '2026-09-25 12:07'
labels: []
dependencies: []
priority: medium
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Scheduler.commit flushes a window in chunks; if a later chunk fails, the retry re-sends the whole window including chunks already delivered, and an OTLP partial success counts as a payload rejection, so accepted records can be re-sent. No doc states the delivery guarantee. Decision (Rob, 2026-09-25): document at-least-once with a dedupe key per event family rather than change the frozen CFO-0025 commit path.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 docs/troubleshooting.md (or a delivery-semantics section) states the at-least-once guarantee, when re-sends happen, and the dedupe attribute for every log and span family
- [ ] #2 A test enumerates every emitted log event family and fails if one lacks its documented dedupe key
- [ ] #3 No change to the commit, checkpoint or flush behaviour in internal/collector
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
