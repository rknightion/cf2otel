package httpreq

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs HTTP request collectors. W5 adds the implementation.
func Register(deps collector.Deps) { _ = deps }
