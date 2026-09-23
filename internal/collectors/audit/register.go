package audit

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the account audit window collector.
func Register(deps collector.Deps) {
	if cfg := deps.Config.Collector("audit.logs"); cfg.Enabled {
		deps.Registry.RegisterWindow(newLogs(deps), cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
