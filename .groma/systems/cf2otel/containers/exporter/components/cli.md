---
type: C4 Component
title: CLI (internal/cli)
status: stable
groma:
  id: cli
  parent: exporter
  code:
    - scanner: go
      file: internal/cli/cli.go
    - scanner: go
      file: internal/cli/owner_unix.go
      symbol: ownedByRuntime
  technology: Go
  group: Process and endpoints
description: Command-line flag parsing and operator modes
---

Parses flags into run options: config path, `--version`, `--healthcheck` (probe the health endpoint), `--validate`, `--print-effective-config` (redacted), `--once`, `--dry-run`, `--reset-state` and the exploration options. Clamps polling intervals to a ten-second minimum and warns when the config file's permissions are too open.
