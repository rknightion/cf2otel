---
id: CFO-0092
title: >-
  aigateway.logs still needs about 97% of a 300s interval per poll and flaps the
  slow-poll and backlog alerts
status: To Do
assignee: []
created_date: '2026-11-05T21:58:00Z'
updated_date: '2026-11-05T21:58:00Z'
labels:
  - bug
dependencies: []
priority: high
ordinal: 122000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured 2026-11-05 on the m7kni stack, build e7c8ae0 (0.20.1-rc.5): aigateway.logs poll elapsed sat at 0.95-0.97 of its 300s interval for most of the evening, so any rise in AI Gateway traffic pushed it over. API calls for the collector rose from about 100 to 2,200 per 15 min between 16:00 and 21:15 UTC while per-call latency stayed flat at about 0.85s and limiter wait at 0.5-1s. Elapsed/interval peaked at 3.2 and backlog/interval at 3.76 around 21:15-21:20, firing cf2otel-collector-slow (12:00, 21:02) and cf2otel-checkpoint-backlog (21:17); it has flapped like this most days since 2026-10-29. Over 6h aigateway.logs made 18,600 of the process's Cloudflare calls, about 0.86 rps and 60% of the total. Stopgap applied on the compose host the same evening: aigateway.logs interval 10m in the host config (previous file kept in the host's cf2otel backups folder). CFO-0085 (runs too slow under the shared limiter) and CFO-0089 (one detail read per log, stop listing at the commit budget) are both In Progress with their live criteria never observed; check first whether the calls-per-log target from CFO-0089 holds live, then decide whether 10m stays or the collector needs more throughput. Do not raise the alert thresholds.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Live calls per exported aigateway.logs record are measured in one fenced epoch and compared with the CFO-0089 target of at most 1.2
- [ ] #2 A decision on the interval is recorded: keep 10m, return to 5m after a throughput fix, or another value, with the measurement it rests on
- [ ] #3 cf2otel-collector-slow and cf2otel-checkpoint-backlog do not fire for aigateway.logs over a 24h window at the chosen interval
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
