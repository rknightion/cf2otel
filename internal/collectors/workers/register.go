package workers

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the Workers overview collector when enabled.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	if c := deps.Config.Collector(workersCollectorName); c.Enabled {
		deps.Registry.RegisterWindow(NewOverviewMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
