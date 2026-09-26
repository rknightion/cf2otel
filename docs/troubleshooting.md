# Troubleshooting

## No records in Loki

Select the OTLP service label first. Other log attributes are structured metadata, so `{event_name="cloudflare.access.login"}` is not a valid stream selector for these logs. Use:

```logql
{service_name="cf2otel"} | event_name="cloudflare.access.login"
```

Check the poller's `cf2otel.scrape.*` metrics and application logs for the collector name. A successful empty window is possible when no matching Cloudflare activity occurred.

## Dry run behavior

Run `-dry-run` with `-once` or with both `-since` and `-before`. Dry run polls Cloudflare but does
not send OTLP requests. It reads the existing checkpoint to choose the window, then keeps checkpoint
advances in memory so the state file remains unchanged and a later run can collect the same window.
It cannot be combined with `-reset-state` or used in daemon mode. `-datasets` also requires a
one-shot or bounded-window run; an unknown collector name reports the valid names.

Each selected collector prints a summary such as `dry-run access.logins: records=4 metrics=7`.
Records count log events and spans; metrics count gauge, counter, and histogram emissions. These
counts describe what the run attempted to emit and are not delivery evidence.

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
