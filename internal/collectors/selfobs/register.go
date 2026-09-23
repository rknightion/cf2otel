package selfobs

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs exporter self-observability.
func Register(deps collector.Deps) {
	if deps.SelfObs == nil {
		return
	}
	if c := deps.Config.Collector("selfobs"); c.Enabled {
		deps.Registry.RegisterSnapshot(deps.SelfObs, c.Interval)
	}
}
