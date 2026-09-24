package dns

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the raw DNS event and Groups-based metrics collectors.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("dns.events"); c.Enabled {
		deps.Registry.RegisterWindow(NewEvents(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector("dns.metrics"); c.Enabled {
		deps.Registry.RegisterWindow(NewMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
