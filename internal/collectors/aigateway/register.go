package aigateway

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
)

const maxExportWindow = 15 * time.Minute

// Register installs the REST-backed AI Gateway collector. GraphQL metrics are
// deliberately absent until its ingestion lag has a safe upper bound.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("aigateway.logs"); c.Enabled {
		window := c.MaxWindow
		// Start with the existing maximum and preserve the configured lookback.
		// Logs opt into scheduler subdivision if captured content exhausts the
		// aggregate commit budget; successful commits keep the learned bound.
		if window > maxExportWindow {
			window = maxExportWindow
		}
		deps.Registry.RegisterWindow(NewLogs(deps.Config, deps.API), c.Interval, c.InitialLookback, window)
	}
	// Off unless configured. The cadence and window are pinned to five minutes
	// so each tick commits and exports one closed window.
	if c := deps.Config.Collector("aigateway.coverage"); c.Enabled {
		deps.Registry.RegisterWindow(NewCoverage(deps.Config, deps.API), coverageWindow, c.InitialLookback, coverageWindow)
	}
}
