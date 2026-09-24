# LOOP.md

Holds loop-specific facts for this repository, read at loop preparation so loop goals cite it
instead of restating it. This is a public repository: no hostname, account/zone/gateway ID, tenant
ID, email or internal-only identifier appears below.

## Gates and commands

- `just check` is the pre-commit gate; run `just` with stdin from `/dev/null` (AGENTS.md).
- `just ci` = `check` plus a goreleaser snapshot (cross-compilation) plus the container image build;
  it needs a Docker daemon, so keep it a separate CI leg from `check` (AGENTS.md, justfile).
- `just gen` / `just gen-check` regenerate and verify the Grafana dashboard and alert manifests from
  `grafana/build_dashboard.py` / `build_rules.py`; `check` already runs `gen-check` (justfile).
- `just vuln` runs govulncheck; `just tidy-check` covers go.mod/go.sum drift; both are inside `check`
  (justfile).
- Never add a Makefile or a shell task wrapper; `just --list` is the command surface (repo-wide
  convention, AGENTS.md pattern).

## Release rules

No per-loop release cap; release per fan-out protocol §7. Every green push to the default branch
also triggers a docs-sync dispatch and an `auto-rc` build; that is expected background activity,
not a signal of anything wrong (loop3 goal).

## Environments and credential conventions

- Config precedence is defaults → YAML → `CF2OTEL_` environment variables, double-underscore nested
  (`CF2OTEL_CLOUDFLARE__API_TOKEN`). Secrets are environment-only and are rejected if found in YAML
  (AGENTS.md).
- Loop work uses three separate Cloudflare credentials: a runtime API token for read-only calls, a
  separate schema-probe token, and a Global API Key used only for read-only GETs against gateway and
  Workers-observability-destination objects plus exactly one narrow write path that a goal must name
  explicitly — never any other Global-Key write (loop3 goal, "Credentials").
- A Grafana Cloud Basic-auth admin token is read from a local credential file for read-only setup
  calls only. Never copy a token, tenant ID, account ID, zone ID, gateway ID, email, IP or hostname
  into the repo, a log, argv, or a fixture — the loop goal carries its own redaction list for exactly
  this (loop3 goal, "Credentials").
- Deploy target is one compose host running one project. A deploy is a version-pin edit plus an
  app-only recreate, verified `healthy` within 10 minutes with an automatic revert of the pin if not
  (loop3 goal, "Root prerequisites"). The host is deliberately not named here.

## Standing route exceptions

None recorded.

## Known traps

- One unentitled GraphQL field fails the whole query. Build every field selection from that zone's
  `settings.availableFields`, and respect `maxNumberOfFields`, `maxDuration` and `notOlderThan`
  (AGENTS.md).
- Adaptive datasets are sampled; take rates from the `*Groups` datasets, never by counting raw rows
  (AGENTS.md).
- Per-request HTTP events carry no Access identity; any inferred identity must be flagged as
  inferred (AGENTS.md).
- The Access REST log only reaches back about a day; a long outage loses that window permanently,
  with no backfill (AGENTS.md).
- Loki receives OTLP log attributes as structured metadata, not stream labels; only `service_name`
  is a stream label. Query with `{service_name="cf2otel"} | event_name="..."` (AGENTS.md).
- Grafana Cloud Tempo on the m7kni stack truncates span attribute values at 2048 characters; a key
  being present proves nothing about its content length (AGENTS.md).
- On an external write's rejection, capture the full response body before deciding whether to roll
  back, and re-GET to confirm a state actually changed before rolling back a write that was itself
  rejected with nothing changed (evidence brief D8).

## Cross-harness eligibility

None approved; ask at preparation.

## Resource mutexes

- The root is the only live Cloudflare API caller; GraphQL is rate-limited platform-wide (loop3
  goal, "Resource mutexes").
- A single process-wide commit mutex serializes windowed checkpoint commits. Catch-up work commits
  further bounded windows sequentially, never concurrently, and only after the previous commit
  succeeded (loop3 goal, decision on catch-up commits).
- The child pool is flat and non-delegating; at most 6 concurrent children in practice, with
  capacity deliberately held back for review (loop3 goal, "Resource mutexes").
- Treat any live external write as a true serial chain: read-only proof, then the one authorised
  write, then deploy. Never run a deploy concurrently with a pending write's proof step (loop3 goal,
  generalized from its serial-dependency chain).

## Grafana stacks

- m7kni stack, named in this repo's own AGENTS.md (the Tempo-truncation trap above). Exact tenant
  IDs, per-signal endpoint hostnames, and account/zone identifiers are not repeated here — they live
  only in the operator's local credential files and the loop's private state (loop3 goal,
  "Credentials").
