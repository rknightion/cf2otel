# Security policy

## Reporting a vulnerability

Please use [GitHub private vulnerability
reporting](https://github.com/rknightion/cf2otel/security/advisories/new), not a public issue.
Include the affected version or commit, a safe reproduction, impact, and redacted configuration.
Never include a Cloudflare API token, an OTLP credential or unredacted log content.

Only the latest release is supported. Fixes are made on `main` and released in the next version;
no older release line receives backports.

## Operational security

- cf2otel only reads from the Cloudflare API. Give it a token with read permission groups only.
- Access, Gateway and AI Gateway logs contain personal data (emails, IP addresses, and prompt and
  completion bodies when body capture is enabled). Treat the telemetry backend as holding that data.
- Secrets are accepted from environment variables only and are redacted from every diagnostic
  surface.
