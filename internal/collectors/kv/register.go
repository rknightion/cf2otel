package kv

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the enabled account-level KV Groups window collectors.
func Register(deps collector.Deps) {
	names := newNameCache()
	for _, spec := range datasets {
		cfg := deps.Config.Collector(spec.collector)
		if cfg.Enabled {
			c := newGroupsCollector(deps.Config, deps.API, spec)
			c.names = names
			deps.Registry.RegisterWindow(c, cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
		}
	}
}
