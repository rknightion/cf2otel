# Loop: cf2otel
tier: guarded
gate: just check
ci-required: ci-success
release-on-push: yes
deploy-on-push: yes
receiver: https://loopwatch.m7kni.com
grafana-stack: robknight

Public repository: no hostname, account/zone/gateway ID, tenant ID, email or internal identifier in
any tracked file or loop artifact. `just ci` needs a Docker daemon. Run `just` in a clean worktree: the gitignored `codex/` holds Go files that fail fmt, lint, vet and test in the shared checkout.

## Credentials

- Three separate Cloudflare credentials: a read-only runtime API token, a schema-probe token, and a
  Global API Key used only for read-only GETs on gateway and Workers-observability-destination
  objects plus one narrow write path that a goal must name. Never any other Global-Key write.
- A Grafana Cloud admin token is read from the local credentials tree for read-only setup calls.
- Never copy a token, ID, email, IP or hostname into the repo, a log, argv or a fixture. A
  diagnostic that reads runtime or compose settings drops every key matching token, key, secret,
  password, authorization or headers and prints key names and value lengths only.
- The compose host runs `:main` (`pull_policy: always`, Watchtower fastlane, 5-minute poll). A
  green push to main is the deploy: the release workflow's `edge` job publishes `:main` and
  Watchtower recreates the app. Confirm the live commit from `cf2otel_build_info_ratio` label `cf2otel_build_commit` on
  robknight; roll back by reverting on main. A new `CF2OTEL_` variable needs its own compose
  `environment:` entry, which is a host edit and needs an ops grant.

## Traps

- GraphQL `settings.availableFields` spells fields `part_field`, not dotted. Edge TTFB and ASN
  dimensions on `httpRequestsAdaptiveGroups` are Pro-only.
- `GET /accounts/{a}/cfd_tunnel` without Cloudflare Tunnel Read returns 200 with an empty list.
  Treat an empty tunnel list as unproven until the token's permissions are confirmed.
- Live proof for a collector is 60 continuous minutes in which the increase of
  `cf2otel_scrape_duration_seconds_count` equals the increase of `cf2otel_scrape_success_total`
  (at least one attempt, one exporter instance, no counter reset), per collector, using
  `codex/live-hour.py` unchanged. A series that exists or a checkpoint that advanced is not proof.
  A counter that never incremented has no series: an absent `cf2otel_scrape_errors_total` is not a
  gap. Before closeout read `time() - cf2otel_scrape_last_success_timestamp_seconds` per collector.
- After a start, the first cycle may fail once on account-scoped collectors; begin an hour proof
  after the first complete cycle and never revert a healthy deploy for it.
- The scheduled "Cloudflare API drift" workflow is not a required check (`ci-success` is). Read
  its log before treating red as a gate or as real drift.
- The drift canary reads only what `restProbeQuery` in `tools/apidrift/probe.go` asks for, mostly
  one row, so a default-paging census does not show what it sees. Two Worker-destination Access
  apps legitimately have no `domain`; JSON null `domain` counts as missing on a domain-based row.
  SCIM update-log `resource_user_email` must appear in at least one row of a page, never every row;
  a nonempty page with none is a canary difference by design, do not suppress it. Null counts as
  missing only for fields in `optional_when_destination_types` or `required_in_any_row`.
- `auto-rc` cuts and publishes a `v*-rc.*` tag for any green `main` SHA, and its rollout restarts
  the app on the same SHA. After a restart, including an in-place restart keeping
  `service_instance_id`, old process series stay visible for the 5-minute lookback with different
  `service_version`. After auto-RC completes, fence the epoch on `cf2otel_build_commit` plus
  `cf2otel_process_start_time_seconds` plus exactly one `service_instance_id`; select the full
  label set, never SHA or instance alone. `loop-pi-audit closeout` attributes release-please branch moves
  and RC tags to their bot actors and exits 0 when every ref is covered; it exits 1 only on an
  ungranted ref (an RC tag, a Renovate branch non-fast-forward). Report each by actor and never
  widen the grants. Merging the release-please PR auto-deletes its branch
  (`delete_branch_on_merge` is on): expected, report it by actor, not as a fault.
- Every push to main redeploys and restarts the process, tracker-only commits included. Hold all
  pushes during an hour proof, and start the hour after `cf2otel_build_commit` matches the landed SHA.
- Launch every watcher under `caffeinate -i`, measure on the wall clock, and treat an observation
  gap as not observed, never as unhealthy. Read the exact-SHA CI run back with `gh run view`.
- The push scan flags every email-shaped literal, `example.com` included, and a net-diff scan
  misses a literal added then removed within one push: use opaque non-email strings and scan newly
  reachable commits too. The private scans allow loopback and the RFC 5737 and RFC 3849 ranges.
- On an external write's rejection, capture the full response body, and re-GET before rolling back.
- The root does not write or repair proof, deploy or scan helpers during a run; a defect goes to a
  lane once, or parks that step.
- A loop lands from a worktree, so the shared checkout's `main` stays put. Never commit there.

## Mutexes

- The root is the only live Cloudflare API caller; REST and GraphQL
  have separate rate budgets (defaults 1.25 rps/burst 5 and 2.5 rps/burst 5), and GraphQL is
  also metered per zone/account (0.9 rps/burst 2, account-based header).
- One process-wide commit mutex serializes windowed checkpoint commits. Catch-up commits run
  sequentially, never concurrently.
- Live external writes are a serial chain: read-only proof, the one authorised write, then deploy.
  Never deploy while a write's proof step is pending.
- Keep review capacity in reserve: at most 6 concurrent children.
