# Security and PII

cf2otel's Cloudflare client is read-only by construction: REST `GET` and GraphQL read queries are the allowed network calls. Give its API token only the read permission groups required by the enabled collectors. AI Gateway full request and response bodies require **AI Gateway Read**; metadata-only read access can list logs but cannot fetch bodies.

Cloudflare and Grafana Cloud tokens must be supplied through environment variables. Configuration rejects `cloudflare.api_token`, `otlp.grafana_cloud.token`, and `otlp.headers.*` in YAML. Keep the configuration and state volume accessible only to the container user and administrators. Avoid printing the environment or copying token values into issue reports.

## Personal data in signals

Access login logs can contain email addresses, user IDs, IP addresses and ray IDs. HTTP event logs can contain client IPs, paths, queries and user agents. AI Gateway logs and traces can include model usage and request metadata. These values belong on log or span records only; metrics use bounded dimensions such as app, model, provider, status class or action. Configure retention and access controls at the OTLP destination for the data you choose to collect.

Audit logs can contain actor email and IP, token identifiers, raw request URI and user agent. Firewall event logs can contain client IP, path, query, user agent and ray ID. Their metrics use bounded action, source and product dimensions only.

HTTP events have no native Access user identity. cf2otel can match a recent login by client IP, host and time; any resulting identity is marked `cloudflare.access.identity.inferred=true`. An ambiguous match stays unattributed. Do not treat an inferred identity as authentication proof.

## AI Gateway bodies

`ai_gateway.capture_bodies` defaults to `false`. Enabling it fetches the full request and response bodies through extra API calls and exports content that may include prompts, completions and other personal data. `ai_gateway.max_body_bytes` caps each captured body; the default is 16 KiB. Set the cap and destination retention deliberately. Body content must never enter cf2otel's own diagnostic logs.

When body capture is enabled, each available request or response body also reaches the configured OTLP destination as a span-correlated log body. Restrict destination access and retention for stored prompts and completions. The log body is capped by `ai_gateway.max_body_bytes`; content stays out of diagnostic logs.

Cloudflare's native AI Gateway trace export can coexist during a comparison, but that duplicates GenAI spans and can duplicate content. After proving cf2otel's mapping, disable only the gateway's native trace export if a single source is required. Workers' other OTLP destinations are a separate setting.
