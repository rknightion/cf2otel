package logpush

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the Logpush health collector when enabled.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	if c := deps.Config.Collector(logpushCollectorName); c.Enabled {
		deps.Registry.RegisterWindow(NewHealthMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
