---
type: C4 System
title: cf2otel
status: stable
groma:
  id: cf2otel
description: Polls Cloudflare read APIs and exports OTLP signals
---

cf2otel is a single Go binary, shipped as a distroless container image. It reads Cloudflare account and zone analytics and logs (Access logins, sampled HTTP requests, AI Gateway requests, audit, firewall and DNS events, Web Analytics, Workers and storage products, and more) on a schedule, and exports them as OpenTelemetry logs, metrics and traces. Signal and attribute names follow `cloudflare.*`, OpenTelemetry `gen_ai.*` for AI Gateway, and `cf2otel.*` for self-observability. It only reads from Cloudflare; checkpoints on local disk let it resume windows after a restart.
