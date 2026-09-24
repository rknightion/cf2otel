# cf2otel

cf2otel polls Cloudflare's read APIs and exports OpenTelemetry logs, metrics and traces over OTLP. Current collectors cover Access login and SCIM activity, Access inventory and login aggregates, sampled HTTP request events for Access-protected hosts, AI Gateway requests, audit logs, firewall events, DNS analytics, Web Analytics and the poller's own health signals.

It is an outbound-only poller. It does not receive traffic from Cloudflare or require Logpush. Its Cloudflare client permits REST `GET` and GraphQL reads only.

Start with [Getting Started](getting-started.md), then use [Configuration](configuration.md) and the [signal reference](signals.md) to choose scope and queries.

Gateway and broader platform analytics remain outside the current collector set.
