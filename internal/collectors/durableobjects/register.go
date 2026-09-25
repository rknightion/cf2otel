package durableobjects

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs each account-level Durable Objects Groups dataset as its
// own window collector so each dataset has an independent checkpoint.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	for _, spec := range durableObjectsDatasets {
		settings := deps.Config.Collector(spec.name)
		if !settings.Enabled {
			continue
		}
		deps.Registry.RegisterWindow(newDatasetCollector(deps.Config, deps.API, spec), settings.Interval, settings.InitialLookback, settings.MaxWindow)
	}
}
