package durableobjects

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs each account-level Durable Objects Groups dataset as its
// own window collector so each dataset has an independent checkpoint.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	names := newNameCache()
	for _, spec := range durableObjectsDatasets {
		settings := deps.Config.Collector(spec.name)
		if !settings.Enabled {
			continue
		}
		c := newDatasetCollector(deps.Config, deps.API, spec)
		c.names = names
		deps.Registry.RegisterWindow(c, settings.Interval, settings.InitialLookback, settings.MaxWindow)
	}
}
