# cf2otel

Cloudflare exporter for OpenTelemetry. cf2otel polls the Cloudflare REST and GraphQL APIs and
exports what Cloudflare otherwise locks behind Enterprise Logpush as OTLP logs, metrics and traces:
Cloudflare Access logins, per-request HTTP events for Access-protected applications, AI Gateway
requests (following the OpenTelemetry GenAI semantic conventions), and the wider account and zone
log surface.

It is a single Go binary with a distroless container image, built for Grafana Cloud.

> **Status:** pre-release scaffold. The first working release is being built now; nothing below
> the scaffold is usable yet.

## Build and check

```sh
just setup
just check
```

## Licence

Apache-2.0. See [LICENSE](LICENSE).
