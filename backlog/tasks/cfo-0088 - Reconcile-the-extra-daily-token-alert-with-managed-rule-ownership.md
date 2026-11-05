---
id: CFO-0088
title: Reconcile the extra daily token alert with managed rule ownership
status: Done
assignee: []
created_date: 2026-10-21 02:01
updated_date: '2026-11-05T23:49:00Z'
labels: []
dependencies: []
priority: medium
type: task
ordinal: 118000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The alert publication workflow applied the generated polling alerts but failed final whole-folder semantic verification because an existing daily input-token alert is outside the repository manifest set. Preserve the existing rule. Decide whether to manage its definition here or give it a separate ownership folder; do not suppress whole-folder verification.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Existing daily token alert intent is preserved and its durable owner is identified
- [x] #2 Alert sync whole-folder semantic verification passes without deleting useful alerts or ignoring arbitrary extras
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reconciled already-landed implementation 3cf7c9b: the daily input-token alert is owned by grafana/build_rules.py and its generated manifest, preserving the rolling 24h 150M input-token threshold and body-derived count fallback. Exact candidate b05d69e passed just check and grafana-sync run 42128369176, including the successful Verify Grafana rules semantically step on 2026-11-05. The verifier still rejects missing, changed and stale extra rules. No code changes needed for this closeout; packaging and new-signal DoD items do not apply.
<!-- SECTION:FINAL_SUMMARY:END -->
