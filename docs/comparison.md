# Comparison with other Cloudflare telemetry paths

| Option | Input and scope | Output | Main trade-off |
| --- | --- | --- | --- |
| cf2otel | Polls non-Enterprise Cloudflare read APIs for Access, HTTP request and AI Gateway data | OTLP logs, metrics and traces | Needs polling checkpoints; raw HTTP events are sampled and Access history is short |
| [cloudflare-log-collector](https://github.com/afreidah/cloudflare-log-collector) | Polls Cloudflare APIs for HTTP, audit, firewall and RUM data | Loki push, Prometheus metrics and OTel traces | Its source coverage and wire formats differ; cf2otel's first release covers only the listed wave-1 domains |
| [cf-log-forwarder](https://github.com/kozliatko/cf-log-forwarder) | Polls Cloudflare APIs for Access, audit, HTTP, firewall and DNS data | CEF over syslog or JSONL | Has persisted cursors and a rich CLI; cf2otel uses OTLP and initially covers Access and HTTP, with other domains planned |
| Cloudflare native AI Gateway OTel export | Emits an AI Gateway request span as requests happen | OTLP traces | No historical backfill and a smaller span attribute set |

cf2otel is useful when Logpush or Enterprise Logpull is unavailable and when a single OTLP pipeline is wanted for the supported datasets. It does not claim a complete HTTP request census: `httpRequestsAdaptive` is sampled. Aggregate request rates come from `httpRequestsAdaptiveGroups`.

Cloudflare's native AI Gateway export sends one `cf.aig.request` span per request with model, provider, token counts, cost and optional input/output messages. cf2otel reconstructs GenAI spans from the AI Gateway log API, adds cache, status, timing and other available request details, and can link a caller trace when `request_head` includes `traceparent`. Body capture is opt-in and capped. Compare the two outputs for your traffic before disabling native export.
