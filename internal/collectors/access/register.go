package access

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs Access collectors. W3 and W4 add the domain implementations.
func Register(deps collector.Deps) { _ = deps }
