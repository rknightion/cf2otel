package aigateway

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the REST-backed AI Gateway collector. GraphQL metrics are
// deliberately absent until its ingestion lag has a safe upper bound.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("aigateway.logs"); c.Enabled {
		deps.Registry.RegisterWindow(NewLogs(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
