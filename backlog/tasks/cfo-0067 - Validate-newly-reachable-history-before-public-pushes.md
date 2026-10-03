---
id: CFO-0067
title: Validate newly reachable history before public pushes
status: Done
assignee:
  - '@loop17-root'
created_date: '2026-10-01 11:17'
updated_date: '2026-10-03 14:14'
labels:
  - security
  - tooling
dependencies: []
priority: high
type: bug
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop14 independent drift review proved a net-tree added-line scan can miss prohibited synthetic email fixtures added in one new commit and removed by a later commit before the same push. The tip was sanitized but both original commits remained published ancestors. No real-person identifier or credential was found in that Git ancestry finding. History rewrite is not authorized. A private loop14 reachability-scan witness correctly rejects the affected historical range, but the durable pre-push validation surface still needs to carry this protection for future work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Pre-push privacy validation checks additions in every newly reachable commit as well as the final tree diff
- [x] #2 An assertion-based fixture adding then removing a prohibited literal is rejected even though its net tree diff is clean
- [x] #3 Validation retains documented fixture exceptions without silently broadening identifier allowlists
- [x] #4 Historical publication exceptions are reported accurately without rewriting history
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Loop15 implementation follows the frozen lane packet; assertion-based red witness, local just check and CodeRabbit, independent REV where required, root linear landing and sustained live evidence where applicable.

Loop17 frozen packet: one local implementation attempt with assertion-red witness, just check and CodeRabbit to terminal result; fresh exact-SHA independent REV before root linear landing. No lane remote writes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Loop16 RECON-M1 checked source criteria 1,2,3 against landed tests at ced4edfbfb66f1b9ee45037bdb425b69820a8ebe. Evidence: codex/evidence-loop16/RECON-return.txt. No new live or browser observation is claimed.

Loop17 fresh REV-P67 FAIL cf7b7f56b0790f0a85147ecf0b42202d55d1997d: allowedAddressClasses reports inner IPv6 loopback suffix as an allowed exception within a complete prohibited address, contradicting historical publication evidence though rejection remains intact. Independent exact-SHA full gate passed and original red/green reporting witness confirmed; prior unrelated DNS assertion failure did not repeat and remains unclassified. Review-repair RR1 of3 now authorized solely for complete-address coverage semantics and public CLI regression; no history rewrite or broader allowlist.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Accurate historical commit/path exception reporting, whole IPv6 and embedded-IPv4 classification, genuine allowed cases, literal nondisclosure and unchanged ancestry proved by authentic CLI reds/greens. Fresh REV-P67-RR2 PASS9eb0623603c43b1b3e0ef85f5a2a0cf9c9cc5406, exact gate/CR coverage. Unchanged commits landedc489845/6070c6e/78ecd03 in pushed652c55e852cd7e27a23fe4c8b3fae6741c58af26; integrated gate and all3scans clean, exactCI pending. Earlier unowned DNS assertion-red remains recorded separately and was not reclassified or weakened.
<!-- SECTION:FINAL_SUMMARY:END -->
