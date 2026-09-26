# cf2otel

cf2otel polls Cloudflare's read APIs and exports OpenTelemetry logs, metrics and traces over OTLP. Its registered collectors cover Access logins and SCIM activity, Access inventory and login aggregates, sampled HTTP request events and HTTP metrics, AI Gateway requests, audit logs, firewall events and metrics, DNS events and metrics, Web Analytics, Gateway DNS, Workers overview, Turnstile, Logpush health, D1, KV, R2, Durable Objects, Queues, Email Routing, Email Sending, and the poller's own health signals.

It is an outbound-only poller. It does not receive traffic from Cloudflare or require Logpush. Its Cloudflare client permits REST `GET` and GraphQL reads only.

Start with [Getting Started](getting-started.md), then use [Configuration](configuration.md) and the [signal reference](signals.md) to choose scope and queries.

AI Gateway request metrics are derived from its REST log collector. The separate GraphQL metrics collector remains disabled while its ingestion lag has no bounded upper limit.
