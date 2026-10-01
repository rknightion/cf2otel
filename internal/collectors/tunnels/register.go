package tunnels

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the opt-in tunnel health snapshot collector.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector("tunnels.status")
	if cfg.Enabled {
		deps.Registry.RegisterSnapshot(&statusCollector{api: deps.API, accountID: deps.Config.Cloudflare.AccountID}, cfg.Interval)
	}
}
