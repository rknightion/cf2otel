---
type: C4 Component
title: Semantic conventions (internal/semconv)
status: stable
groma:
  id: semconv
  parent: exporter
  code:
    - scanner: go
      file: internal/semconv/semconv.go
    - scanner: go
      file: internal/semconv/access.go
    - scanner: go
      file: internal/semconv/audit.go
    - scanner: go
      file: internal/semconv/certs.go
    - scanner: go
      file: internal/semconv/deny_registry.go
    - scanner: go
      file: internal/semconv/dex.go
    - scanner: go
      file: internal/semconv/dns.go
    - scanner: go
      file: internal/semconv/email.go
    - scanner: go
      file: internal/semconv/firewall.go
    - scanner: go
      file: internal/semconv/gateway.go
    - scanner: go
      file: internal/semconv/genai.go
    - scanner: go
      file: internal/semconv/healthchecks.go
    - scanner: go
      file: internal/semconv/http.go
    - scanner: go
      file: internal/semconv/loadbalancers.go
    - scanner: go
      file: internal/semconv/metadata.go
    - scanner: go
      file: internal/semconv/platform.go
    - scanner: go
      file: internal/semconv/rum.go
    - scanner: go
      file: internal/semconv/selfobs.go
    - scanner: go
      file: internal/semconv/statuspage.go
    - scanner: go
      file: internal/semconv/tunnels.go
    - scanner: go
      file: internal/semconv/warp.go
    - scanner: go
      file: internal/semconv/workersai.go
  technology: Go
  group: Collection core
description: Every signal and attribute name, and their cardinality rules
---

The only place a signal or attribute name is declared, one file per domain. Cloudflare signals are `cloudflare.*`, AI Gateway follows the OpenTelemetry `gen_ai.*` conventions, and self-observability is `cf2otel.*`. Cardinality is decided here, not at the call site: IPs, emails, paths, ray IDs, user agents, prompt IDs and session IDs are log or span attributes only, and metric attributes are bounded enums or configured names. Also holds the deny-list registry of names operators may suppress.
