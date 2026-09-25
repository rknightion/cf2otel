package kv

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the enabled account-level KV Groups window collectors.
func Register(deps collector.Deps) {
	for _, spec := range datasets {
		cfg := deps.Config.Collector(spec.collector)
		if cfg.Enabled {
			deps.Registry.RegisterWindow(newGroupsCollector(deps.Config, deps.API, spec), cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
		}
	}
}
