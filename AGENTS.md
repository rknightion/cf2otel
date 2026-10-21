# cf2otel

Polls the Cloudflare REST and GraphQL APIs and exports Access logins, per-request HTTP events, AI
Gateway requests and the wider non-Enterprise log surface as OTLP logs, metrics and traces.

## Task interface

`just check` is the gate and must pass before you commit. `just ci` adds the goreleaser snapshot
and the container image build. Run `just` with stdin from `/dev/null`.

Off the Mac, when the remote gate runner is installed (`which gaterun`): `gaterun -- just ci` (or
`just check`), paced across lanes with `gaterun-slot <lane> -- gaterun -- just ci`; `.gaterunner.yaml`
is its contract. Run one test or package locally.

## Tracker

Tasks are `CFO-NNNN` in `backlog/`. **Cloudflare API surface - live-verified reference** is the data
contract for every collector, and **Parity checklist - reference pollers** is the feature floor.

Loops: the protocol lives in `~/repos/agent-docs/sources/loop/` (this board's "Loop protocol pointer" doc points there); this repository's loop facts are in `LOOP.md`.

`backlog/` is committed and public: no credential, account/zone/gateway ID, email, public IP or
internal hostname in a task or doc.

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
- AI Gateway token counts can fall back to complete, untruncated captured response JSON.
  Positive gateway counts remain authoritative; `cloudflare.ai_gateway.usage_source`
  marks body-derived log/span values and is never a metric dimension.
- Grafana Cloud Tempo on the m7kni stack stores span attribute values truncated at 2048 characters.
  An attribute key present in Tempo proves nothing about its content length.

<!-- BACKLOG.MD GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.50.1 -->
<CRITICAL_INSTRUCTION>

## Backlog.md Workflow

This project uses Backlog.md for task and project management.

**For every user request in this project, run `backlog instructions overview` before answering or taking action.**

Use the overview to decide whether to search, read, create, or update Backlog tasks.

Before task lifecycle actions, read the matching detailed guide:
- `backlog instructions task-creation` before creating or splitting tasks
- `backlog instructions task-execution` before planning, changing status or assignee, adding a plan or implementation notes, or implementing task work
- `backlog instructions task-finalization` before checking acceptance criteria, writing final summaries, or moving tasks to terminal statuses

Use `backlog <command> --help` before running unfamiliar commands. Help shows options, fields, and examples.

Do not edit Backlog task, draft, document, decision, or milestone markdown files directly. Use the `backlog` CLI so metadata, relationships, and history stay consistent.

</CRITICAL_INSTRUCTION>
<!-- BACKLOG.MD GUIDELINES END -->

## Loop facts

- `LOOP.md` - read at loop preparation: gates, release rules, environments and credential conventions, standing route exceptions, traps, cross-harness eligibility, resource mutexes and Grafana stacks for this repository's loops.
