---
id: CFO-0001
title: >-
  Freeze exporter seams: config, semconv, telemetry, collector framework, cfapi
  skeleton
status: To Do
assignee: []
created_date: '2026-09-23 10:04'
labels:
  - 'wave:1'
  - seam
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Root-owned pre-fan-out pass. Freeze the shared seams every wave-1 lane codes against, modelled on polylens2otel (layout, koanf config, Emitter) and tailscale2otel's Snapshot/Window collector split with file checkpoints. Split every registry into per-domain stub files with a frozen call list in cmd/cf2otel/collectors_import.go.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 internal/config loads defaults, YAML and CF2OTEL_ env (double-underscore nesting), rejects secrets found in YAML, validates all errors at once, and redacts secrets in every dump
- [ ] #2 internal/semconv declares cloudflare.*, gen_ai.* and cf2otel.* names; nothing outside it declares a signal or attribute name
- [ ] #3 internal/telemetry exposes the Emitter (gauge, counter, histogram, log event, span with explicit start/end timestamps and links) and is the only OTLP importer, with Grafana Cloud basic auth over OTLP http and grpc
- [ ] #4 internal/collector provides SnapshotCollector and WindowCollector registration, a staggered scheduler with panic recovery, and an atomic same-directory file checkpoint store; a test proves a window is neither skipped nor double-counted across a restart
- [ ] #5 One stub Register file per wave-1 domain (access, httpreq, aigateway, inventory, selfobs) exists and is called from cmd/cf2otel/collectors_import.go; just check passes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check (fmt-check, lint, vet, test, tidy-check, build, vuln)
- [ ] #2 just ci before a change that touches the Dockerfile, goreleaser or the image (adds snapshot + image)
- [ ] #3 Every new signal or attribute name declared in internal/semconv and listed in docs/signals.md
<!-- DOD:END -->
