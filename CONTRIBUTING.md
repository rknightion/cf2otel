# Contributing

`just check` is the local acceptance gate and CI runs it verbatim. It covers formatting, lint, vet,
the race-enabled test suite, `go mod tidy` drift, the build and govulncheck.

- Every signal and attribute name is declared in `internal/semconv` and nowhere else.
- A value the Cloudflare API did not return stays absent from the output. Never emit a zero or an
  empty string in its place.
- Test fixtures are sanitized before they are written: no token, account or zone ID, email address,
  public IP address or internal hostname. Use RFC 5737 / RFC 3849 documentation addresses and
  `example.com` names.
- A cursor change needs a test that proves no window is skipped or counted twice.

## Public push scan

Before publishing, run `just push-scan PUBLISHED_BASE CANDIDATE_HEAD </dev/null` (replace both revision placeholders).
The base is the previously published commit, not the candidate's parent. The scanner checks added
lines in every commit reachable from head but not base (including merged branches and merge
resolutions), then checks added lines in the net base-to-head diff. An add-then-remove sequence is
still rejected. It does not rewrite history or override a publication exception.

The standard-library-only tool flags 32-hex identifiers, email-shaped literals (including example
addresses), valid IPv4/IPv6 addresses outside the RFC 5737 / RFC 3849 documentation ranges, and
Cloudflare token prefixes `cfut_`, `cfapi_` and `v1.0-`. Unprefixed tokens and other sensitive names
need the external literal list: set `PUSHSCAN_LITERALS` to a local file containing one exact literal
per line. Empty lines are ignored; CRLF is accepted. The list is never checked in or printed.
Literal matching is case-sensitive. This is a content gate, not a complete secret detector.

Exit status is 0 for clean, 1 for findings, and 2 for configuration/Git errors. Findings print only
file, commit (or `net-diff`) and class, never the matching value or source line. Git errors are
redacted. The tool reads local Git objects only and scans binary diffs as text too.

There are no built-in fixture exceptions. A reviewed exception must name an exact repository path
and a nonempty reason in a JSON object, passed using
`go run ./tools/pushscan --allowlist LOCAL_JSON PUBLISHED_BASE CANDIDATE_HEAD`
(replace the file and revision placeholders).
Wildcards and directory-wide exceptions are not accepted; each named file is excluded from all
classes, both history and net diff. Keep this list narrow and review it alongside the candidate.
Tests construct prohibited values at runtime rather than checking them in.

Use Conventional Commits (`feat:`, `fix:`, `docs:` and so on); release-please builds the changelog
from them. The project is licensed under Apache-2.0.
