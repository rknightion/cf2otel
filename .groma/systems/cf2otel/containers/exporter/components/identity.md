---
type: C4 Component
title: Identity index (internal/identity)
status: stable
groma:
  id: identity
  parent: exporter
  code:
    - scanner: go
      file: internal/identity/index.go
    - scanner: go
      file: internal/identity/types.go
  technology: Go
  group: Collection core
description: Correlates Access logins with HTTP requests
---

An in-memory, time-bounded index of Access logins by client IP and host. The Access collector feeds it and the HTTP request collector looks requests up in it. Per-request HTTP events carry no Access identity, so a match is always inferred and is flagged as inferred on the emitted signal.
