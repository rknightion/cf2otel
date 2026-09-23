package firewall

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the raw firewall event and Groups-only metrics collectors.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("firewall.events"); c.Enabled {
		deps.Registry.RegisterWindow(NewEvents(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector("firewall.metrics"); c.Enabled {
		deps.Registry.RegisterWindow(NewMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
