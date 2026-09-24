package rum

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs account-level RUM metric collectors when enabled.
func Register(deps collector.Deps) {
	if cfg := deps.Config.Collector("rum.pageloads"); cfg.Enabled {
		deps.Registry.RegisterWindow(NewPageloads(deps.Config, deps.API), cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
	if cfg := deps.Config.Collector("rum.web_vitals"); cfg.Enabled {
		deps.Registry.RegisterWindow(NewWebVitals(deps.Config, deps.API), cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
