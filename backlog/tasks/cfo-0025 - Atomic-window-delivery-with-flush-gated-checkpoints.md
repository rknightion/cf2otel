---
id: CFO-0025
title: Atomic window delivery with flush-gated checkpoints
status: In Progress
assignee: []
created_date: '2026-09-23 16:15'
updated_date: '2026-09-23 21:22'
labels:
  - 'wave:2'
  - collector
dependencies: []
priority: high
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Wave 1 shipped duplicate telemetry: Loki held 224 AI Gateway rows for 71 unique log IDs (up to six copies), and the same re-emission double-counted the request, cost and token counters for the affected windows. Cause, confirmed in source: a window collector emits as it goes (aigateway.logs fetches per-row detail and bodies inside its emit loop; httpreq.events emits per zone), so a failure part-way through a window has already emitted the earlier rows, the checkpoint does not advance, and the next tick emits them again. Separately, the scheduler persists the checkpoint as soon as records are enqueued in the OTel SDK, before the OTLP export succeeds, so an exporter outage can lose a window silently. Decision 2026-09-23 (Rob): buffer each window, gate the checkpoint on a successful flush, and prove it live on camden with a restart and a deliberately failing OTLP endpoint. Resumes CFO-0008 AC4 and CFO-0011 AC3.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every window collector's output is buffered and reaches the exporter only after all of the window's fetches succeed; a fetch failure part-way through a window emits nothing, proven by tests for aigateway.logs and httpreq.events
- [x] #2 A checkpoint advances only after the window's logs and spans are force-flushed and the exporter reports success, and the window's metric increments are applied only after that success; a test against an OTLP endpoint returning 503 leaves the checkpoint unchanged, and the retry after recovery delivers each record exactly once
- [x] #3 A failed commit in one collector cannot let another collector advance its checkpoint past records that the failed flush dropped; a concurrent test proves it
- [ ] #4 Live on camden with nonempty AI Gateway source activity: a container restart, and a deliberately failing OTLP endpoint for at least one window, each end with every source log ID in the affected windows present exactly once in Loki, quoted with the exact queries and counts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [x] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [x] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Wave 2: buffer each collector window, force-flush logs and spans before metrics and checkpoint,

Then prove restart and full exporter outage on the deployment host.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Wave 2 implementation verified at a3fa01b: buffered collector windows, flush-gated checkpoints, post-flush metric application, and concurrent failure isolation; just check and just ci passed in cf2otel-verify. AC4 awaits live P1/P2.

Wave 2 P1 at v0.2.3, 18:08:34-19:08:35 UTC restart window: source 16 IDs, Loki 16 rows, each ID once and no extras; exact-ID Tempo search finds 13 spans and misses three 19:00-19:01 UTC requests. HTTP request rows exist but their Loki metadata lacks ray IDs, so the duplicate-ray check has no inputs. AC4 remains open; P2 fault window began 21:09:23 UTC with pre-state captured and a bounded restore script.

Correction: direct trace readback through correlated Loki content trace IDs proves all 16/16 P1 source IDs have exactly one Tempo span; the earlier 13/16 was a Tempo search false negative. The 16/16 Loki count remains. P1 HTTP ray-ID duplicate check is unproven because 50 HTTP rows carried no ray ID metadata in this window. P2 remains in progress; AC4 stays open pending its result.
<!-- SECTION:NOTES:END -->
