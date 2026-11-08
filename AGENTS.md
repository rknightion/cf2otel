# cf2otel

Polls the Cloudflare REST and GraphQL APIs and exports Access logins, per-request HTTP events, AI
Gateway requests and the wider non-Enterprise log surface as OTLP logs, metrics and traces.

## Task interface

`just check` is the gate and must pass before you commit. `just ci` adds the goreleaser snapshot
and the container image build. Run `just` with stdin from `/dev/null`.

Off the Mac, when the remote gate runner is installed (`which gaterun`): `gaterun -- just ci` (or
`just check`), paced across lanes with `gaterun-slot <lane> -- gaterun -- just ci`; `.gaterunner.yaml`
is its contract. Run one test or package locally.

## Review and architecture map

- Before committing a non-trivial change, run `just review` and read `.review/report.md`: duplication
  and complexity the change introduced. Advisory; act on what is real.
- `.groma/` is the Groma architecture map (one component per package). `just groma-check` gates CI
  and the loop. A new or deleted non-test `.go` file under `cmd/` or `internal/` needs
  `just groma-scan`, then `npx -y groma.md@<groma_version> edit <package-component> --combine <new-id>`
  (or `remove <id>`), committed with the change. Never hand-edit `.groma/` except to drop a code
  reference to a deleted file, which the CLI cannot remove.

## Tracking work

Work is tracked in this repository's GitHub issues; they are the only queue. Do not add a checklist
file such as `ROADMAP.md` beside them.

- A pull request body says how it relates to an issue, on a line of its own: `Closes #N` with the
  `Acceptance evidence` table from `.github/pull_request_template.md` (one row for each acceptance
  criterion of that issue), `Refs #N` when it only relates to one, or `Task: none, <reason>`.
- A pull request that changes `.github/workflows/` or `.github/actions/` carries no closing keyword:
  write `Task: none, <reason>`, with `Refs #N` beside it if you like.
- Contributors never need the project board or the tracker tooling; a maintainer handles both.
- Issues and pull requests are public, so they carry no real identifiers: no email addresses,
  account, zone or gateway ids, host names or addresses. Write the shape, not the instance.
- Older commits, docs and tests cite legacy board ids such as `CFO-0001`. `just issue <ID>` prints
  the issue each one became; `.github/tracker/id-map.json` is the map. A bare `#N` of 110 or below
  is a pull request or the Renovate Dependency Dashboard.

## Reference docs

- `.github/tracker/history/doc-0003 - Cloudflare-API-surface-live-verified-reference.md` is the data
  contract for every collector. Read it before changing a collector's query, fields or paging. Code
  and docs cite it as `doc-0003`.
- `.github/tracker/history/doc-0004 - Parity-checklist-reference-pollers.md` is the feature floor
  for the reference pollers.
- `.github/tracker/history/` holds the retired board's documents. They are history: do not add to
  it, and keep new reference material under `docs/` when it is fit for the published site.

## Ownership seams

- `internal/config` owns the koanf configuration surface. Precedence is defaults, YAML, then
  `CF2OTEL_` environment variables with **double underscores** for nesting
  (`CF2OTEL_CLOUDFLARE__API_TOKEN`). Secrets are environment-only and rejected if found in YAML.
- `internal/semconv` owns every signal and attribute name. Nothing else declares one. Cloudflare
  signals are `cloudflare.*`, GenAI signals follow the OpenTelemetry `gen_ai.*` conventions, and
  self-observability is `cf2otel.*`.
  Cardinality is decided there, not at the call site: IPs, emails, paths, ray IDs, user agents,
  prompt IDs and session IDs are log/span attributes only, never metric attributes. Metric attributes
  are bounded enums or configured names.
- `internal/telemetry` is the only package that touches OTLP.
- `internal/cfapi` is the only package that talks to Cloudflare. It is read-only by construction.
- `internal/collector` owns registration, scheduling, windows and checkpoints.
- Each collector domain lives in `internal/collectors/<domain>/` and exposes `Register(collector.Deps)`.
  `cmd/cf2otel/collectors_import.go` is the frozen domain call list.

## Traps

- The distroless runtime requires the eMudhra trust-store fix: `ca-certificates`
  `20250419+deb13u1` or newer. Debian 12 distroless digests can still contain the
  affected bundle; verify package metadata rather than assuming a fresh digest is patched.

- One unentitled field fails a whole GraphQL query. Build selections from `settings.availableFields`
  per zone, and respect `maxNumberOfFields`, `maxDuration` and `notOlderThan`.
- Adaptive datasets are sampled. Rates come from `*Groups` datasets, never from counting raw rows.
- Per-request HTTP events carry no Access identity; the inferred identity is always flagged as
  inferred.
- The Access REST log reaches back only about a day. A long outage loses data; nothing can backfill it.
- Loki receives OTLP log attributes as structured metadata, not stream labels. Only `service_name`
  is a stream label, so query `{service_name="cf2otel"} | event_name="..."`.
- Poll health uses actual registered intervals: `cf2otel.scrape.elapsed` retains the larger
  of active elapsed and last completed duration; `cf2otel.checkpoint.backlog` subtracts
  current source holdback, including dynamic bucket alignment. Startup cursor observation
  is distinct from the commit-only checkpoint callback.
- AI Gateway log detail `request_head`/`response_head` are body text as JSON strings (not
  headers); `*_head_complete=true` means the head is the whole body. Capture reads a raw
  body only for an incomplete head, so a typical log costs one REST read. The live limit is
  per token, `Ratelimit-Policy` `q=1200;w=300`. REST and GraphQL share a
  combined 3.75 rps/burst 5 ceiling; REST can use spare capacity. Explicit old REST overrides
  still apply. Judge recovery by declining checkpoint age as well as scrape success.
- AI Gateway budgeted detail reads overlap across source seconds, but emission and checkpoints
  remain ordered whole-second operations. Three-row lookahead shares a two-request physical
  read cap, including raw-body fallbacks, to preserve competing GraphQL throughput.
  Boundary reuse is limited to six entries, 16MiB
  and five minutes from fetch; cursor changes and failed-delivery replays discard reuse.
- AI Gateway detail `collect_log_payload=false` explicitly disables body storage: null heads
  then lead to 404/code7002 on both raw endpoints. Skip those reads while preserving detail
  metadata and unavailable-body accounting; missing/null flags still require fallback reads.
- AI Gateway token counts can fall back to complete, untruncated captured response JSON.
  Positive gateway counts remain authoritative; `cloudflare.ai_gateway.usage_source`
  marks body-derived log/span values and is never a metric dimension.
- The OTLP protobuf marshal rejects a string with invalid UTF-8 before any request is sent, and one
  such record fails its whole commit window on every retry. Byte caps stop on a rune boundary
  (`runeBoundary`) and `internal/telemetry` repairs any remaining invalid string at emission.
- Each bounded commit chunk flushes source logs and traces concurrently, joins both
  acknowledgements and checks observed export failures before replay or checkpoint advancement.
- Completed poll diagnostics use an operational trace pipeline separate from source delivery.
  API header/limiter durations are overlapping request sums, not wall-time phases; committed
  items count checkpointed windows and telemetry records, not AI source rows.
- The model-specific daily `jev` reconciliation counts outer `workers-ai` response usage
  once per gateway/log ID. Nested `typesafe` records and delivery replays otherwise double-count
  this workload. Generic token counters remain provider-operation totals, not deduplicated billing.
- The drift canary reads only what `restProbeQuery` in `tools/apidrift/probe.go` asks for, mostly
  one row, so a default-paging census does not show what it sees. Two Worker-destination Access
  apps legitimately have no `domain`; JSON null `domain` counts as missing on a domain-based row.
  SCIM update-log `resource_user_email` must appear in at least one row of a page, never every row;
  a nonempty page with none is a canary difference by design, do not suppress it. Null counts as
  missing only for fields in `optional_when_destination_types` or `required_in_any_row`.
- Every rule in the Grafana "cf2otel alerts" folder comes from `grafana/build_rules.py`. A rule
  made by hand there fails grafana-sync's post-publish check as a stale live rule.
- Grafana Cloud Tempo on the m7kni stack stores span attribute values truncated at 2048 characters.
  An attribute key present in Tempo proves nothing about its content length.

## Loop facts

- `LOOP.md` - read at loop preparation: gates, release rules, environments and credential conventions, standing route exceptions, traps, cross-harness eligibility, resource mutexes and Grafana stacks for this repository's loops.
