package logpush

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs enabled Logpush collectors; failure job labels require opt-in.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	if c := deps.Config.Collector(failuresCollectorName); c.Enabled {
		deps.Registry.RegisterWindow(&failureMetrics{cfg: deps.Config, api: deps.API}, c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector(logpushCollectorName); c.Enabled {
		deps.Registry.RegisterWindow(NewHealthMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
