---
id: doc-0002
title: Wave operating model
type: guide
created_date: '2026-09-23 09:59'
updated_date: '2026-09-23 10:01'
---
This document carries **only what is true of cf2otel**. The campaign model itself (run contract,
routing, authority, lane briefs, contract freezing, blockers, goal template, run-end protocol,
pre-flight checklist) is the *Agent fan-out protocol (canonical)* doc, and that doc wins on any
specific. If a section below could be pasted into another repo unchanged, it is in the wrong document.

Every rule keeps its reason. A rule without one gets argued away by the next session.

## Read-only against Cloudflare is a product gate

cf2otel issues `GET` requests and the GraphQL `POST` only. The Cloudflare client refuses any other
method before network I/O, and a test proves it. The runtime token carries read permission groups
only. The one sanctioned Cloudflare write in the project's life is a root-only action named in a wave
goal (for example switching off the native AI Gateway trace export), done with the operator's
credential outside the binary.

## The live API is the contract; documentation is a hint

`doc-0003` records what the API did on a real account. Where Cloudflare's docs disagree, the API
wins, and a lane must not "correct" working code to match a doc. GraphQL selections are built from
`settings.availableFields`, never from a hard-coded field list alone.

## Fixtures are sanitized before they are written, and are append-only

The repository is public. No token, account ID, zone ID, gateway ID, email address, public IP,
internal hostname, ray ID or prompt/completion content from the real account reaches a fixture, the
tracker or a doc. Use RFC 5737 / RFC 3849 documentation addresses, `example.com` names and invented
IDs of the right shape. Raw live captures stay in the gitignored `codex/` directory. Once committed, a
fixture is never overwritten or regenerated; add a new one beside it.

## Cardinality is decided in `internal/semconv`, not at the call site

IPs, emails, paths, ray IDs, user agents, prompt IDs and session IDs are log/span attributes only,
never metric attributes. Metric attributes are bounded enums or configured names (zone, app, gateway,
model, provider, status class, action).

## Identity inference is labelled, never presented as fact

Per-request HTTP events carry no Access identity. cf2otel infers it from Access login events and
stamps the inference flag beside it. A dashboard or alert that drops the flag misrepresents the data.

## Work that touches a live system stays on the root

Lanes do local edits, tests and inventory sweeps. SSH to camden, deploys, pushes, GitHub actions,
OpenBao writes, Cloudflare writes and Grafana Cloud queries and pushes stay with the root: a lane
inherits the parent's permission mode and cannot clear a soft block. A blocked lane is run by the
root, never re-dispatched.

## Commits go straight to `main`, staged and committed by explicit pathspec

No branches or PRs for this repo's own work; release-please owns the only PR. `git add -A` and
`git commit -a` are forbidden; use `git commit -- <paths>`. CodeRabbit reviews code before the commit.
