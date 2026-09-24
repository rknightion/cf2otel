package gateway

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the Gateway DNS Groups collector when enabled.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("gateway.dns"); c.Enabled {
		deps.Registry.RegisterWindow(NewDNSMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
