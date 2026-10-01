// Package healthchecks collects opt-in zone health-check analytics.
package healthchecks

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs health-check analytics only after explicit origin-label opt-in.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector("healthchecks.events")
	if cfg.Enabled {
		deps.Registry.RegisterWindow(&events{cfg: deps.Config, api: deps.API}, cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
