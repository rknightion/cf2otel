# cf2otel

Cloudflare exporter for OpenTelemetry. cf2otel polls the Cloudflare REST and GraphQL APIs and
exports what Cloudflare otherwise locks behind Enterprise Logpush as OTLP logs, metrics and traces:
Cloudflare Access logins, per-request HTTP events for Access-protected applications, AI Gateway
requests (following the OpenTelemetry GenAI semantic conventions), and the wider account and zone
log surface.

It is a single Go binary with a distroless container image, built for Grafana Cloud.
The first release collects Access, HTTP request, AI Gateway and account inventory signals.
The AI Gateway GraphQL metrics poller remains disabled while its ingestion lag is measured;
the REST log poller supplies AI Gateway metrics in the meantime.

See the [getting started guide](docs/getting-started.md), [configuration](docs/configuration.md)
and [signal catalogue](docs/signals.md).

## Build and check

```sh
just setup
just check
```

## Licence

Apache-2.0. See [LICENSE](LICENSE).
