# Security and PII

cf2otel's Cloudflare client is read-only by construction: REST `GET` and GraphQL read queries are the allowed network calls. Give its API token only the read permission groups required by the enabled collectors. AI Gateway full request and response bodies require **AI Gateway Read**; metadata-only read access can list logs but cannot fetch bodies.

Cloudflare and Grafana Cloud tokens must be supplied through environment variables. Configuration rejects `cloudflare.api_token`, `otlp.grafana_cloud.token`, and `otlp.headers.*` in YAML. Keep the configuration and state volume accessible only to the container user and administrators. Avoid printing the environment or copying token values into issue reports.

## Prometheus endpoint

The optional `/metrics` listener is disabled by default and binds `127.0.0.1:9465`
when enabled. It has no authentication or TLS. Existing metric attributes can expose
sensitive operational information; explicitly binding `:9465` makes that information
reachable on every interface. Restrict scraping with network policy or an authenticated
reverse proxy before widening the bind or publishing a container port. The endpoint
exports only the existing metrics, not logs, traces, resource metadata or Go/process
metrics. OTLP remains required and active. See [configuration](configuration.md#optional-prometheus-pull-endpoint).

## Cloudflare permission groups

The table maps every configured collector name to its expected read group. For GraphQL collectors, use `Account Analytics Read` for account datasets and `Analytics Read` for zone datasets. Scope each token to only the account and zones it needs. These group names follow Cloudflare's [API token permissions](https://developers.cloudflare.com/fundamentals/api/reference/permissions/), [GraphQL Analytics token setup](https://developers.cloudflare.com/analytics/graphql-api/getting-started/authentication/api-token-auth/), and the [Audit Logs v2 endpoint](https://developers.cloudflare.com/api/resources/accounts/subresources/logs/subresources/audit/methods/list/).

**Unverified** means the least required group has not been confirmed by a live permission check recorded in the project's Cloudflare API reference (`doc-0003`). The reference records live permission behavior only for AI Gateway: metadata access lists logs, while body fetches require `AI Gateway Read`. Dataset and field entitlements can still vary by account and zone.

| Collector | Cloudflare permission group(s) | Evidence |
| --- | --- | --- |
| `loadbalancers.health` | `Load Balancing: Monitors and Pools Read` (Account); `Analytics Read` (Zone) for pool traffic; `Zone Read` for account-filtered zone discovery | The existing opt-in toggle now collects pool health and trailing-window traffic together. Health-only tokens may lose refresh when required zone discovery or analytics reads fail; absent or explicitly unentitled traffic datasets are no-ops. Empty pool accounts only list pools. Pool permission is doc-derived from the official [pool source](https://developers.cloudflare.com/api/resources/load_balancers/subresources/pools/methods/list/); least-privilege verification and populated health remain unproved. No Write grant needed. |
| `dex.tests` | `Cloudflare DEX Read` (Account) | Doc-derived read permission accepted from official [HTTP results](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/http_tests/methods/get/) and [traceroute results](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/traceroute_tests/methods/get/) sources; no live least-privilege grant or populated detail verification. Disabled by default; do not grant a Write permission for this read-only collector. |
| `access.logins` | `Access: Audit Logs Read` (Account) | Unverified |
| `access.login_metrics` | `Account Analytics Read` (Account) | Unverified |
| `access.scim` | `Access: SCIM Logs Read` (Account) | Unverified |
| `inventory.access` | `Access: Apps Read` and `Access: Users Read` (Account) | Unverified |
| `access.seats` | `Access: Users Read` (Account), the same users endpoint used by `inventory.access` | User-list seat fields verified by read-only contract canary; permission name unverified |
| `warp.fleet` | `Cloudflare DEX Read` (Account) preferred; documented alternatives `Zero Trust Read` or `Zero Trust Report` | Doc-derived [device list method](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dex/subresources/fleet_status/subresources/devices/methods/list/). Root observed only successful empty rows; populated shape and least privilege remain unverified. Do not grant the documented Write alternative for this read-only source. |
| `tunnels.status` | `Cloudflare Tunnel Read` (Account) | Loop 14 preparation verified the required group; without it the list can be an empty 200, so an empty result does not prove permission |
| `httpreq.events` | `Analytics Read` (Zone); `Access: Apps Read` (Account) when `http.scope` is `access_protected` | Unverified |
| `httpreq.threats` | `Analytics Read` (Zone) | Free/Pro rollup entitlement verified; least permission group unverified. (CFO-0046.05) |
| `httpreq.transfer` | `Account Analytics Read` (Account) | Account aggregate, no account ID or host labels; least permission group unverified. (CFO-0046.05) |
| `logpush.failures` | `Account Analytics Read` (Account) and `Analytics Read` (Zone) | Disabled by default; explicit opt-in to numeric job-ID labels, bounded by the platform series cap. Destination/status/final flags only, never raw error text; zone names rather than zone IDs. Least permission groups unverified. (CFO-0047.03) |
| `healthchecks.events` | `Analytics Read` (Zone) | Disabled by default; Pro entitlement verified, Free disabled and skipped. Explicit origin-label opt-in: bounded non-IP FQDN or human name, never origin IP or health-check ID. Omit timings without usable identity. Least permission group unverified; this seam registers no collector yet. (CFO-0050.03) |
| `httpreq.metrics` | `Analytics Read` (Zone) | Unverified |
| `aigateway.logs` | `AI Gateway Metadata Read` (Account); add `AI Gateway Read` (Account) when body capture is enabled | Live-verified in `doc-0003` |
| `aigateway.metrics` | `Account Analytics Read` (Account); collector is disabled while GraphQL Groups ingestion lag is unbounded | Unverified |
| `audit.logs` | `Account Settings Read` (Account) | Unverified |
| `firewall.events` | `Analytics Read` (Zone) | Unverified |
| `firewall.metrics` | `Analytics Read` (Zone); add `Zone WAF Read` for opt-in rule descriptions | Runtime enrichment not live-enabled by this change; ruleset read shapes verified in root preparation |
| `dns.events` | `Analytics Read` (Zone) | Unverified |
| `dns.metrics` | `Analytics Read` (Zone) | Unverified |
| `rum.pageloads` | `Account Analytics Read` (Account) | Unverified |
| `rum.web_vitals` | `Account Analytics Read` (Account) | Unverified |
| `gateway.dns` | `Account Analytics Read` (Account) | Unverified |
| `workers.overview` | `Account Analytics Read` (Account) | Unverified |
| `workersai.metrics` | `Account Analytics Read` (Account) | Disabled by default; account aggregate only, no model/resource ID or tag labels. Runtime selection succeeded, but least-privilege permission group remains unverified. |
| `workers.invocations` | `Account Analytics Read` (Account) | Unverified |
| `turnstile.events` | `Account Analytics Read` (Account) | Unverified |
| `logpush.health` | `Account Analytics Read` (Account) | Unverified |
| `d1.analytics` | `Account Analytics Read` (Account); additional database-list read entitlement unknown | Runtime list returned 200 in root preparation; least privilege and canary-token entitlement unverified |
| `d1.queries` | `Account Analytics Read` (Account); additional database-list read entitlement unknown | Same read-only D1 name lookup; least privilege and canary-token entitlement unverified |
| `d1.storage` | `Account Analytics Read` (Account); additional database-list read entitlement unknown | Same read-only D1 name lookup; least privilege and canary-token entitlement unverified |
| `kv.operations` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Runtime list returned 200 in root preparation; least privilege and canary-token entitlement unverified |
| `kv.storage` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Same read-only KV name lookup; least privilege and canary-token entitlement unverified |
| `r2.bandwidth` | `Account Analytics Read` (Account) | Unverified |
| `r2.catalog_data` | `Account Analytics Read` (Account) | Unverified |
| `r2.catalog_maintenance` | `Account Analytics Read` (Account) | Unverified |
| `r2.operations` | `Account Analytics Read` (Account) | Unverified |
| `r2.storage` | `Account Analytics Read` (Account) | Unverified |
| `r2.sql` | `Account Analytics Read` (Account) | Unverified |
| `durableobjects.invocations` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Runtime list returned 200 in root preparation; least privilege and canary-token entitlement unverified |
| `durableobjects.periodic` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Same read-only Durable Objects name lookup; least privilege and canary-token entitlement unverified |
| `durableobjects.sql_storage` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Same read-only Durable Objects name lookup; least privilege and canary-token entitlement unverified |
| `durableobjects.subrequests` | `Account Analytics Read` (Account); additional namespace-list read entitlement unknown | Same read-only Durable Objects name lookup; least privilege and canary-token entitlement unverified |
| `queues.backlog` | `Account Analytics Read` (Account); additional queue-list read entitlement unknown | Runtime list returned 200 in root preparation; least privilege and canary-token entitlement unverified |
| `queues.consumer` | `Account Analytics Read` (Account); additional queue-list read entitlement unknown | Same read-only Queue name lookup; least privilege and canary-token entitlement unverified |
| `queues.delayed_backlog` | `Account Analytics Read` (Account); additional queue-list read entitlement unknown | Same read-only Queue name lookup; least privilege and canary-token entitlement unverified |
| `queues.message_operations` | `Account Analytics Read` (Account); additional queue-list read entitlement unknown | Same read-only Queue name lookup; least privilege and canary-token entitlement unverified |
| `email.routing` | `Analytics Read` (Zone) | Unverified |
| `email.sending` | `Analytics Read` (Zone) | Unverified |
| `certs.packs` | `SSL and Certificates Read` (Zone) | Live-verified in the loop 14 endpoint schema; disabled by default |
| `selfobs` | None; this collector reads local process state only | Not applicable |

The expected groups for other collectors are candidates based on their API surface and GraphQL dataset scope. Verify them against the target account before enabling a collector; Cloudflare can require dataset-specific entitlements in addition to the base analytics group.

## Platform resource-name boundaries

Name enrichment uses only GET `/accounts/{account}/d1/database`, `/accounts/{account}/storage/kv/namespaces`, `/accounts/{account}/queues` and `/accounts/{account}/workers/durable_objects/namespaces`. Root preparation verified runtime-token access (200), not the minimal permission group or the canary token's access. No new permission name is asserted and no Write grant is recommended. A failed lookup preserves metric counts in `other` and is cached for one hour, replacing expired names. Only complete lists within 100 pages/5000 rows are published. Source IDs remain internal and never enter metric labels or fallback values. Resolved names are limited to 128 characters and 49 sticky normal complete attribute sets plus one remainder per metric; R2 action/bucket pairs share this cap. Resource names can contain operator-defined sensitive content; assess them and destination access before export. Existing depth quantile selections are unchanged.

## Personal data in signals

`dex.tests` uses bounded test-name/kind labels only. Test and account IDs are
internal request data, never metric attributes or error contents. Names are
restricted to a 128-character ASCII human-name alphabet; unsafe and reserved
names use `other`, but a syntactically safe name can still contain sensitive
content. Review names and destination access before enabling the collector.
Its total series cap has six reserved signal/kind remainders, whose arithmetic
means are not device-weighted statistics. No test target URL, device identity,
IP, raw result body, log or trace is emitted. Catalog ambiguity fails without
falling back to an ID label.

`access.seats` decodes only the two boolean flags from each user row and retains only aggregate counts across pages. User identifiers, emails, names, IPs and device data are not parsed into the collector's row model, stored in a catalog, or attached to its metrics. Its only metric attribute is the string `cloudflare.access.seat.type`, with values `access` and `gateway`. The HTTP client still receives the upstream response subject to its existing response-size limit; this collector does not log response bodies.

Access login logs can contain email addresses, user IDs, IP addresses and ray IDs. HTTP event logs can contain client IPs, paths, queries and user agents. AI Gateway logs and traces can include model usage and request metadata. These values belong on log or span records only; metrics use bounded dimensions such as app, model, provider, status class or action. Configure retention and access controls at the OTLP destination for the data you choose to collect.

Audit logs can contain actor email and IP, token identifiers, raw request URI and user agent. Firewall event logs can contain client IP, path, query, user agent and ray ID. Their metrics default to bounded action, source and product dimensions. Firewall metrics may opt in to rule ID/description, host and client country, bounded by a collector-side total per-window series cap. Rule descriptions and hosts can expose operator-defined names; review their content and destination access before enabling `firewall.rule_dimensions`. Lookup failure never blocks metric counts.

HTTP events have no native Access user identity. cf2otel can match a recent login by client IP, host and time; any resulting identity is marked `cloudflare.access.identity.inferred=true`. An ambiguous match stays unattributed. Do not treat an inferred identity as authentication proof.

## AI Gateway bodies

`ai_gateway.capture_bodies` defaults to `false`. Enabling it fetches the full request and response bodies through extra API calls and exports content that may include prompts, completions and other personal data. `ai_gateway.max_body_bytes` caps each captured body; the default is 16 KiB. Set the cap and destination retention deliberately. Body content must never enter cf2otel's own diagnostic logs.

When body capture is enabled, each available request or response body also reaches the configured OTLP destination as a span-correlated log body. Restrict destination access and retention for stored prompts and completions. The log body is capped by `ai_gateway.max_body_bytes`; content stays out of diagnostic logs.

Cloudflare's native AI Gateway trace export can coexist during a comparison, but that duplicates GenAI spans and can duplicate content. After proving cf2otel's mapping, disable only the gateway's native trace export if a single source is required. Workers' other OTLP destinations are a separate setting.

## Opt-in HTTP metric enrichment

`colo`, `asn` and `error_path` breakdowns are off by default and use the existing zone
Analytics Read permission and `httpRequestsAdaptiveGroups` dataset. No additional REST
lookup or permission is required. Each feature selects only its complete advertised field
set within the zone's field budget. ASN and its organization description were live-verified
as strings on one eligible HTTP Groups zone (API reference section 18); unavailable ASN
fields skip that feature without selecting unentitled fields or suppressing base metrics.

ASN descriptions are organization metadata, not Access identity. Review their content and
the configured safe route names before enabling export. An optional host allowlist restricts
only the three new features, not the base HTTP scope. New labels never include a host,
request path, query, IP, user agent, ray ID or resource identifier. Paths are decoded and
normalized only for exact internal template lookup, then discarded; only a validated
configured name is emitted. Invalid/unmatched paths retain counts in a safe `other` bucket.
Validation errors do not echo configured route templates, names or hosts. Per-window caps
and one count-preserving remainder bound these metrics; they do not bound cumulative SDK
series growth across windows. Dashboard/UI and live-counter verification are separate from
local source validation.

`loadbalancers.health` emits only bounded configured pool names. Pool/account IDs
are internal GET path inputs, never labels, errors or fallback names; origin addresses
and RTT strings are not emitted. Ambiguous names fail safely. `other` reports the
minimum of known excluded flags, not complete fleet availability. No resource is
created and health detail is explicitly fixture-only/unprobed in the canary.
