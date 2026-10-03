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
- The deploy host's compose file passes named variables only (explicit `environment:` entries, no
  `env_file:`). A new `CF2OTEL_` variable needs its own compose `environment:` entry; a line added to
  `.env` alone never reaches the process (loop 9 preparation).

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
- The operator's Mac idle-sleeps during overnight loops, which freezes every root-launched watcher
  mid-observation. A deploy watcher that overran its deadline this way once forced the rollback of a
  healthy deploy. Launch every watcher under `caffeinate -i`, measure deadlines on the wall clock
  (not `time.monotonic()`, which stops during sleep on macOS), and treat an observation gap or an SSH
  error as "not observed", never as unhealthy (loop 10 preparation).
- On Codex, a watcher backgrounded with `nohup … &` from a one-shot exec call dies when that call
  returns: empty log, no traceback, no receipt (loops 7, 9 and 10; both loop 10 deploy watchers).
  Start a watcher as a foreground command in a retained exec session (`exec_command` with a short
  `yield_time_ms`, record the returned `session_id`), and have it rewrite its receipt after every
  observation so a death still leaves an observation count (loop 11 preparation).
- The drift canary reads only what `restProbeQuery` in `tools/apidrift/probe.go` asks for, and most
  entries ask for one row (`per_page=1` or `limit=1`). A census taken with the API's default paging
  does not show what the canary sees (loop 10 REV-41).
- The drift canary's Access apps entry: two Worker-destination apps (destination types `worker`
  and `all_preview_workers`) legitimately have no `domain`. A domainless row of any other shape is
  drift (loop 10 preparation, CFO-0041). An explicit JSON null `domain` counts as missing on a
  domain-based row (Rob, 2026-09-27, CFO-0044).
- SCIM update-log rows: `resource_user_email` is absent on every GROUP row and on some USER rows.
  The owner's rule is that it must appear in at least one row of the page, never on every row
  (Rob, 2026-09-27, CFO-0042). A nonempty page with no email on any row, such as a group-only sync
  hour, is therefore a canary difference by design; do not "fix" it by suppressing the check (Rob
  kept the rule at loop 12 preparation over a CodeRabbit major). SCIM traffic is sparse: 47 of 48
  hourly canary windows were empty in the loop 12 preparation census.
- JSON null counts as missing only for fields named in `optional_when_destination_types` or
  `required_in_any_row`; plain `required_fields` checks keep null-as-present, and empty strings are
  not covered (Rob, 2026-09-27, CFO-0044).
- The exact-SHA CI watcher is `codex/watch-ci.py <full-sha> <receipt-dir>`, verified against a
  completed SHA at loop 13 preparation. Loop 12's hand-edited copy dropped `for x in runs` from its
  `all()`, and the swallowed NameError ran every wait to its 60-minute deadline. A receipt holding
  only error observations means "not observed": read the run back with `gh run view`.
- `just check` in the shared checkout fails in fmt, lint, vet and test because the gitignored `codex/`
  holds Go files; a clean worktree of the same SHA passes. Run every gate in a worktree or `-runend`
  (loop 14 preparation).
- GraphQL `settings.<dataset>.availableFields` spells fields `part_field` (`sum_edgeResponseBytes`,
  `dimensions_coloCode`), not dotted; a dotted lookup reads every field as absent (loop 14 preparation).
- On `httpRequestsAdaptiveGroups`, edge TTFB (avg and quantiles) and ASN dimensions are Pro-only, while
  origin-duration quantiles, bytes, visits, country, protocol, TLS and content type are available on Free
  (live, loop 14 preparation).
- `GET /accounts/{a}/cfd_tunnel` without `Cloudflare Tunnel Read` returns `200` with an empty list, not a
  403; `Zero Trust Read` does not cover it. Treat an empty tunnel list as unproven until the token's
  permissions are confirmed (loop 14 preparation; both loop tokens now carry it).
- A series that exists, or a checkpoint that advanced across two reads, is not live proof. A collector
  passes live only on 60 continuous minutes in which its attempts equal its successes: the increase of
  `cf2otel_scrape_duration_seconds_count` equals the increase of `cf2otel_scrape_success_total`, with at
  least one attempt, one exporter instance and no counter reset. `codex/live-hour.py` is the witness; use
  it unchanged (Rob, 2026-10-02). The rule is per collector: a criterion about one collector closes on that
  collector's own clean 60 minutes in the receipt, and another collector's failure in the same hour does not
  block it (loop 17 preparation; loop 16 parked every criterion on one Durable Objects mismatch).
- A counter that never incremented has no series. An absent `cf2otel_scrape_errors_total` series for a
  collector is not a coverage gap and never blocks an hour proof: a failed attempt shows as attempts ahead
  of successes. Loop 15 parked every live criterion on this for a whole run (loop 16 preparation).
- Before closeout, read `time() - cf2otel_scrape_last_success_timestamp_seconds` for every collector.
  A value near the current Unix time means the collector has never succeeded on the running version
  (loop 15 preparation).
- release-please regenerates its branch with a non-fast-forward move at every release, and
  `loop-pi-audit closeout` exits 1 on any non-fast-forward move even when the ref is granted. Report
  it under `### Blocked` as the bot's move with both SHAs; never widen the grants or rerun the audit
  to get a clean exit (loop 14 closeout).
- A diagnostic that reads runtime, exporter or compose settings drops every key matching token, key,
  secret, password, authorization or headers before it writes or prints anything. Print key names and
  value lengths only (loop 14 incident).
- The push scan flags every email-shaped literal, `example.com` included, and a net-diff scan misses a
  literal added in one commit and removed in a later one of the same push. Use opaque non-email
  strings for identity fixtures, and scan newly reachable commits as well as the net diff
  (`codex/scan-history-added-loop14.py` is the private witness until CFO-0067 lands) (loop 14).
- The operator's Mac also suspends the root session itself: loop 14 lost 5.5 hours with a tool call
  in flight. Start the launcher under `caffeinate -i` (loop 15 preparation).
- On an external write's rejection, capture the full response body before deciding whether to roll
  back, and re-GET to confirm a state actually changed before rolling back a write that was itself
  rejected with nothing changed (evidence brief D8).
- The root does not write or repair proof, deploy or scan helpers during a run. It runs the scripts the
  preparation names, unchanged apart from the version, digest and path values they take as arguments. A
  defect in one goes to a lane once, or parks that step. Loop 15 spent its proof window and three repair
  cycles on helpers it wrote itself (loop 16 preparation).
- The private scan scripts print allowed hits (loopback `127.0.0.0/8`, the RFC 5737 and RFC 3849
  documentation ranges). Only a hit they do not mark allowed blocks a push (Rob, 2026-10-02).
- On 0.13.0 the first cycle after a start failed once on 27 account-scoped collectors, error class
  `other`, and every later cycle succeeded. Start an hour proof after the first complete cycle, and never
  revert a healthy deploy for that one failure (loop 16 preparation).
- `auto-rc` cuts a `v*-rc.*` tag and pre-release for any green `main` SHA, including one pushed before
  the run. The closeout grants cover every `v*-rc.*` tag whose target is on `main`'s first-parent history
  (Rob, 2026-10-02).
- A loop lands from `-runend`, so the shared checkout's `main` stays where START left it. A commit made there
  during a run (loop 16: a LOOP.md receiver opt-in) diverges from `origin/main` and blocks `git pull --ff-only`.
  START rebases only a docs-only commit it can attribute and records it; anything else parks START (loop 17
  preparation).
- The state record is written in plain sentences with spaces between words. Removing spaces to fit a size
  cap makes it unreadable after compaction; archive detail instead (loop 16 preparation).

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

wave-notify receiver: https://loopwatch.m7kni.com
