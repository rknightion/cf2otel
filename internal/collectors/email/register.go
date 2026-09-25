package email

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the zone-scoped Email Routing and Email Sending Groups collectors.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector(routingSpec.name); c.Enabled {
		deps.Registry.RegisterWindow(newMetrics(deps.Config, deps.API, routingSpec), c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector(sendingSpec.name); c.Enabled {
		deps.Registry.RegisterWindow(newMetrics(deps.Config, deps.API, sendingSpec), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
