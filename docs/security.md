# Security and PII

cf2otel's Cloudflare client is read-only by construction: REST `GET` and GraphQL read queries are the allowed network calls. Give its API token only the read permission groups required by the enabled collectors. AI Gateway full request and response bodies require **AI Gateway Read**; metadata-only read access can list logs but cannot fetch bodies.

Cloudflare and Grafana Cloud tokens must be supplied through environment variables. Configuration rejects `cloudflare.api_token`, `otlp.grafana_cloud.token`, and `otlp.headers.*` in YAML. Keep the configuration and state volume accessible only to the container user and administrators. Avoid printing the environment or copying token values into issue reports.

## Cloudflare permission groups

The table maps every configured collector name to its expected read group. For GraphQL collectors, use `Account Analytics Read` for account datasets and `Analytics Read` for zone datasets. Scope each token to only the account and zones it needs. These group names follow Cloudflare's [API token permissions](https://developers.cloudflare.com/fundamentals/api/reference/permissions/), [GraphQL Analytics token setup](https://developers.cloudflare.com/analytics/graphql-api/getting-started/authentication/api-token-auth/), and the [Audit Logs v2 endpoint](https://developers.cloudflare.com/api/resources/accounts/subresources/logs/subresources/audit/methods/list/).

**Unverified** means the least required group has not been confirmed by a live permission check recorded in the project's Cloudflare API reference (`doc-0003`). The reference records live permission behavior only for AI Gateway: metadata access lists logs, while body fetches require `AI Gateway Read`. Dataset and field entitlements can still vary by account and zone.

| Collector | Cloudflare permission group(s) | Evidence |
| --- | --- | --- |
| `access.logins` | `Access: Audit Logs Read` (Account) | Unverified |
| `access.login_metrics` | `Account Analytics Read` (Account) | Unverified |
| `access.scim` | `Access: SCIM Logs Read` (Account) | Unverified |
| `inventory.access` | `Access: Apps Read` and `Access: Users Read` (Account) | Unverified |
| `httpreq.events` | `Analytics Read` (Zone); `Access: Apps Read` (Account) when `http.scope` is `access_protected` | Unverified |
| `httpreq.metrics` | `Analytics Read` (Zone) | Unverified |
| `aigateway.logs` | `AI Gateway Metadata Read` (Account); add `AI Gateway Read` (Account) when body capture is enabled | Live-verified in `doc-0003` |
| `aigateway.metrics` | `Account Analytics Read` (Account); collector is disabled while GraphQL Groups ingestion lag is unbounded | Unverified |
| `audit.logs` | `Account Settings Read` (Account) | Unverified |
| `firewall.events` | `Analytics Read` (Zone) | Unverified |
| `firewall.metrics` | `Analytics Read` (Zone) | Unverified |
| `dns.events` | `Analytics Read` (Zone) | Unverified |
| `dns.metrics` | `Analytics Read` (Zone) | Unverified |
| `rum.pageloads` | `Account Analytics Read` (Account) | Unverified |
| `rum.web_vitals` | `Account Analytics Read` (Account) | Unverified |
| `gateway.dns` | `Account Analytics Read` (Account) | Unverified |
| `workers.overview` | `Account Analytics Read` (Account) | Unverified |
| `turnstile.events` | `Account Analytics Read` (Account) | Unverified |
| `logpush.health` | `Account Analytics Read` (Account) | Unverified |
| `d1.analytics` | `Account Analytics Read` (Account) | Unverified |
| `d1.queries` | `Account Analytics Read` (Account) | Unverified |
| `d1.storage` | `Account Analytics Read` (Account) | Unverified |
| `kv.operations` | `Account Analytics Read` (Account) | Unverified |
| `kv.storage` | `Account Analytics Read` (Account) | Unverified |
| `r2.bandwidth` | `Account Analytics Read` (Account) | Unverified |
| `r2.catalog_data` | `Account Analytics Read` (Account) | Unverified |
| `r2.catalog_maintenance` | `Account Analytics Read` (Account) | Unverified |
| `r2.operations` | `Account Analytics Read` (Account) | Unverified |
| `r2.storage` | `Account Analytics Read` (Account) | Unverified |
| `r2.sql` | `Account Analytics Read` (Account) | Unverified |
| `durableobjects.invocations` | `Account Analytics Read` (Account) | Unverified |
| `durableobjects.periodic` | `Account Analytics Read` (Account) | Unverified |
| `durableobjects.sql_storage` | `Account Analytics Read` (Account) | Unverified |
| `durableobjects.subrequests` | `Account Analytics Read` (Account) | Unverified |
| `queues.backlog` | `Account Analytics Read` (Account) | Unverified |
| `queues.consumer` | `Account Analytics Read` (Account) | Unverified |
| `queues.delayed_backlog` | `Account Analytics Read` (Account) | Unverified |
| `queues.message_operations` | `Account Analytics Read` (Account) | Unverified |
| `email.routing` | `Analytics Read` (Zone) | Unverified |
| `email.sending` | `Analytics Read` (Zone) | Unverified |
| `selfobs` | None; this collector reads local process state only | Not applicable |

The expected groups for other collectors are candidates based on their API surface and GraphQL dataset scope. Verify them against the target account before enabling a collector; Cloudflare can require dataset-specific entitlements in addition to the base analytics group.

## Personal data in signals

Access login logs can contain email addresses, user IDs, IP addresses and ray IDs. HTTP event logs can contain client IPs, paths, queries and user agents. AI Gateway logs and traces can include model usage and request metadata. These values belong on log or span records only; metrics use bounded dimensions such as app, model, provider, status class or action. Configure retention and access controls at the OTLP destination for the data you choose to collect.

Audit logs can contain actor email and IP, token identifiers, raw request URI and user agent. Firewall event logs can contain client IP, path, query, user agent and ray ID. Their metrics use bounded action, source and product dimensions only.

HTTP events have no native Access user identity. cf2otel can match a recent login by client IP, host and time; any resulting identity is marked `cloudflare.access.identity.inferred=true`. An ambiguous match stays unattributed. Do not treat an inferred identity as authentication proof.

## AI Gateway bodies

`ai_gateway.capture_bodies` defaults to `false`. Enabling it fetches the full request and response bodies through extra API calls and exports content that may include prompts, completions and other personal data. `ai_gateway.max_body_bytes` caps each captured body; the default is 16 KiB. Set the cap and destination retention deliberately. Body content must never enter cf2otel's own diagnostic logs.

When body capture is enabled, each available request or response body also reaches the configured OTLP destination as a span-correlated log body. Restrict destination access and retention for stored prompts and completions. The log body is capped by `ai_gateway.max_body_bytes`; content stays out of diagnostic logs.

Cloudflare's native AI Gateway trace export can coexist during a comparison, but that duplicates GenAI spans and can duplicate content. After proving cf2otel's mapping, disable only the gateway's native trace export if a single source is required. Workers' other OTLP destinations are a separate setting.
