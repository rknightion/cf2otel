---
type: C4 Container
title: cf2otel exporter
status: stable
groma:
  id: exporter
  parent: cf2otel
  technology: Go, OpenTelemetry Go SDK
description: 'The cf2otel process: scheduler, Cloudflare client and OTLP export'
---

One long-running Go process (or a one-shot run with `--once`). The entry point loads configuration, builds the Cloudflare API client and the OTLP providers, registers every enabled collector domain, and hands them to the scheduler. The scheduler polls each collector on its interval, buffers each time window completely, flushes it through OTLP and only then advances that collector's checkpoint in a JSON file under the state directory (default `/var/lib/cf2otel`). Optional loopback health and unauthenticated Prometheus pull endpoints run alongside.
