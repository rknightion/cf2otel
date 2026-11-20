# cf2otel review guide

## Software and entry points

cf2otel polls the Cloudflare REST and GraphQL APIs and exports Access logins, HTTP events, AI Gateway requests and other account signals as OTLP logs, metrics and traces. Start at `cmd/cf2otel` (`collectors_import.go` is the frozen list of collector domains). Each domain lives in `internal/collectors/<domain>` and exposes `Register(collector.Deps)`; `internal/collector` owns scheduling, windows and checkpoints.

## Ownership seams

- `internal/semconv` declares every signal and attribute name. A name declared anywhere else is a defect. Metric names and attributes are published contracts, so a rename needs a migration.
- Metric attributes are bounded enums or configured names. IPs, emails, paths, ray IDs, user agents, prompt IDs and session IDs belong on logs and spans only.
- `internal/cfapi` is the only package that talks to the Cloudflare API and stays read-only. A new write call or a second Cloudflare client is a defect.
- `internal/telemetry` is the only package that touches OTLP.
- `internal/config` loads defaults, then YAML, then `CF2OTEL_` environment variables with double underscores for nesting. Secrets are environment-only and must be rejected if found in YAML.

## Behaviour to check

- GraphQL: one unentitled field fails the whole query, so selections come from `availableFields` per zone and respect `maxNumberOfFields`, `maxDuration` and `notOlderThan`. Adaptive datasets are sampled: rates come from the `*Groups` datasets, never from counting raw rows.
- Rate limiting: REST and GraphQL share one limiter in `internal/cfapi`. Check changes to bursts, rates or admission order against the window budgets, and that waiters are not starved or lost on cancellation.
- Windows and checkpoints: a checkpoint advances only after the window's logs and traces are acknowledged. Look for retries that double-count, replays that skip data, and a checkpoint that moves past a failed export.
- Strings reaching OTLP: protobuf marshal rejects invalid UTF-8 and one bad record fails its whole commit window on every retry. Byte caps must stop on a rune boundary (`runeBoundary` in `internal/collectors/aigateway`), and `internal/telemetry` repairs what remains.
- Network failures: HTTP/2 pooled connections must be replaced after an outage in both the Cloudflare client and the OTLP exporters, not reused.
- Identity on per-request HTTP events is always inferred and must stay flagged as inferred.

## Generated files

`dashboards/cf2otel.json`, `alerts/grafana-managed/*.json` and `alerts/grafana-managed/fixtures/*.test.yaml` are written by `grafana/build_dashboard.py` and `grafana/build_rules.py` (`just gen`) and are marked `linguist-generated`, so Bugbot does not read them. Review the generator and its inputs instead; `just gen-check` catches drift. Alert rules are never made by hand in Grafana: a hand-made rule in the cf2otel alerts folder fails the sync check.

## Public repository

Issues, pull requests, fixtures and docs are public. Flag any real hostname, account, zone or gateway ID, email address, token or tenant name. Test data must be synthetic.

## Verification

The gate is `just check </dev/null`: format, golangci-lint, vet, race tests, tidy, build, govulncheck, generated drift and the deployment verifier tests. `just groma-check` gates the architecture map in `.groma/`; a new or deleted non-test `.go` file under `cmd/` or `internal/` needs `just groma-scan` and a curated component edit in the same change. CI also runs the privacy sweep, acceptance evidence and workflow security checks.

A behaviour fix needs a test that fails without it. Recent fixes show the pattern: the limiter fairness fix added a fairness test, and the silenced-connection fix added blackhole tests in `internal/cfapi` and `internal/telemetry` plus new outage alert fixtures. Treat a changed test expectation, a loosened threshold or a skipped test as a finding until the code change explains it. Comments should name a reproducible regression, missing evidence or a gate that lost coverage, not style.
