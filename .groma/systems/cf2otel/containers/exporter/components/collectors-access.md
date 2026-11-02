---
type: C4 Component
title: Access
status: stable
groma:
  id: collectors-access
  parent: exporter
  code:
    - scanner: go
      file: internal/collectors/access/register.go
      symbol: Register
    - scanner: go
      file: internal/collectors/access/graphql.go
    - scanner: go
      file: internal/collectors/access/logins.go
    - scanner: go
      file: internal/collectors/access/scim.go
    - scanner: go
      file: internal/collectors/access/seats.go
  technology: Go
  group: Collector domains
description: Access logins, SCIM updates, login metrics and seat counts
---

Reads the Access request log (`access.logins`, which reaches back only about a day), login metrics, the SCIM update log and a paginated users snapshot for seat counts. Login rows also feed the identity index that the HTTP request collector uses.

## Relationships

| Source | Target | Description | Technology |
| --- | --- | --- | --- |
| [internal/collectors/access/logins.go](../../../../../../internal/collectors/access/logins.go) | [internal/cfapi/client.go](../../../../../../internal/cfapi/client.go) | Queries Cloudflare | cfapi REST/GraphQL |
| [internal/collectors/access/logins.go](../../../../../../internal/collectors/access/logins.go) | [internal/identity/index.go](../../../../../../internal/identity/index.go) | Records logins | Go call |
