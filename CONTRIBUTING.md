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

Use Conventional Commits (`feat:`, `fix:`, `docs:` and so on); release-please builds the changelog
from them. The project is licensed under Apache-2.0.
