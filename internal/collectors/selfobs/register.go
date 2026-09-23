package selfobs

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs exporter self-observability. W9 adds the implementation.
func Register(deps collector.Deps) { _ = deps }
