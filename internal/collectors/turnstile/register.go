package turnstile

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the Turnstile events collector when enabled.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	if c := deps.Config.Collector(turnstileCollectorName); c.Enabled {
		deps.Registry.RegisterWindow(NewEventMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
