package access

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs Access collectors in the frozen domain order.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("access.logins"); c.Enabled {
		deps.Registry.RegisterWindow(newLogins(deps), c.Interval, c.InitialLookback, c.MaxWindow)
	}
	registerLoginMetrics(deps)
	registerSCIM(deps)
}
