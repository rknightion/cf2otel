package inventory

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs Access inventory collectors. W4 adds the implementation.
func Register(deps collector.Deps) { _ = deps }
