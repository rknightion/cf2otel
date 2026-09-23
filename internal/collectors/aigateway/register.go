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
		// A three-hour catch-up can exceed the 90-second OTLP commit deadline
		// because each captured body adds a separate bounded log export. Keep
		// the configured lookback, but commit it in smaller windows.
		if window > maxExportWindow {
			window = maxExportWindow
		}
		deps.Registry.RegisterWindow(NewLogs(deps.Config, deps.API), c.Interval, c.InitialLookback, window)
	}
}
