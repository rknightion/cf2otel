---
id: CFO-0084
title: Correct the build-commit label and release-branch deletion in LOOP.md
status: Done
assignee: []
created_date: '2026-10-07 06:54'
updated_date: '2026-10-07 06:54'
labels:
  - loop
  - docs
dependencies: []
type: docs
ordinal: 114000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Loop32 read the deployed commit from a label named build_commit and found it absent; its closeout audit also flagged the release-please branch deletion on merge as ungranted. The emitted OTLP attribute cf2otel.build.commit arrives in Mimir as cf2otel_build_commit (the generated dashboard already uses it), and the repository deletes merged head branches automatically.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 LOOP.md names the deployed-commit label cf2otel_build_commit everywhere it previously said build_commit
- [x] #2 LOOP.md states that merging the release-please PR auto-deletes its branch and that this is expected, reported by actor
- [x] #3 One GET-only read of cf2otel_build_info_ratio on the m7kni stack shows cf2otel_build_commit present with the deployed release SHA
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 loop-lint loopmd LOOP.md exits 0 and LOOP.md stays under 6 KB
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live read 2026-10-07 (one request, curl exit 0, no retry): cf2otel_build_info_ratio carries cf2otel_build_commit=d167771522ee8e13189fe8dff2f6d5132367c16f, cf2otel_build_version=0.18.0, service_version=0.18.0. This is the v0.18.0 merge SHA, so loop32's inconclusive comparison resolves to a match; the label name, not the deployment, was wrong. loop-lint loopmd exit 0; LOOP.md 5949 bytes.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Renamed the deployed-commit label in LOOP.md to cf2otel_build_commit (three places) and recorded the release-please branch auto-delete as expected. Verified by one live read showing cf2otel_build_commit equal to the v0.18.0 merge SHA, and loop-lint loopmd exit 0.
<!-- SECTION:FINAL_SUMMARY:END -->
