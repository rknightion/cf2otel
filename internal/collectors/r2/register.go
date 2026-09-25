package r2

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs the enabled account-scoped R2 Groups collectors.
func Register(deps collector.Deps) {
	for _, spec := range r2Datasets {
		cfg := deps.Config.Collector(spec.collector)
		if !cfg.Enabled {
			continue
		}
		deps.Registry.RegisterWindow(&datasetCollector{cfg: deps.Config, api: deps.API, spec: spec}, cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
