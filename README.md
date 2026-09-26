# cf2otel

Cloudflare exporter for OpenTelemetry. cf2otel polls the Cloudflare REST and GraphQL APIs and
exports what Cloudflare otherwise locks behind Enterprise Logpush as OTLP logs, metrics and traces:
Cloudflare Access logins, per-request HTTP events for Access-protected applications, AI Gateway
requests (following the OpenTelemetry GenAI semantic conventions), and the wider account and zone
log surface.

It is a single Go binary with a distroless container image, built for Grafana Cloud.
Collectors cover Access, HTTP requests, AI Gateway, audit logs, firewall and DNS events, Web Analytics,
Gateway DNS, Workers overview, Turnstile, Logpush health, D1, KV, R2, Durable Objects, Queues, Email
Routing and Sending, and the poller's own health signals. AI Gateway GraphQL metrics remain disabled
while ingestion lag is unbounded; the REST log collector supplies AI Gateway metrics.

See the [getting started guide](docs/getting-started.md), [configuration](docs/configuration.md)
and [signal catalogue](docs/signals.md).

## Build and check

```sh
just setup
just check
```

## Licence

Apache-2.0. See [LICENSE](LICENSE).
