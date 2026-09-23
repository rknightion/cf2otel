package aigateway

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs AI Gateway collectors. W7 and W8 add the implementations.
func Register(deps collector.Deps) { _ = deps }
