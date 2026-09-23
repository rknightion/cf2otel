# Troubleshooting

## No records in Loki

Select the OTLP service label first. Other log attributes are structured metadata, so `{event_name="cloudflare.access.login"}` is not a valid stream selector for these logs. Use:

```logql
{service_name="cf2otel"} | event_name="cloudflare.access.login"
```

Check the poller's `cf2otel.scrape.*` metrics and application logs for the collector name. A successful empty window is possible when no matching Cloudflare activity occurred.

## GraphQL query fails or returns less than expected

An unentitled field fails the entire Cloudflare GraphQL query. Entitlement varies by zone. cf2otel selects from each zone's `settings.availableFields` and respects its field count, window and retention limits. A permission error can also be transient just after a token is created. Adaptive request events are sampled; rates come from the Groups dataset, not raw row counts.

## Access login gap after an outage

The REST Access log returned only about 30 hours of history in live verification. Preserve checkpoints and poll frequently. Increasing `initial_lookback` cannot recover rows no longer available through that API.

## HTTP event has no user

Per-request Cloudflare HTTP events have no Access identity. Enable `identity.enabled` and the Access login collector for inferred attribution. The match requires compatible host, IP and time and intentionally leaves ambiguous events unattributed.

## AI Gateway bodies are empty

The log list response normally has empty `request` and `response` strings. Body capture is off by default and needs the AI Gateway Read permission, `capture_bodies: true`, and separate body fetches. Review [Security and PII](security.md) before enabling it.

## Container is unhealthy

The health listener is loopback-only. Confirm that the config file and persistent state directory are readable or writable respectively by UID 65532, and that both secrets and the required account and OTLP settings are present.
