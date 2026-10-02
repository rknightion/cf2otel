package workersai

import (
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Register installs the opt-in Workers AI aggregate metrics collector.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	settings := deps.Config.Collector(semconv.CollectorNameWorkersAIMetrics)
	if !settings.Enabled {
		return
	}
	deps.Registry.RegisterWindow(&metricsCollector{cfg: deps.Config, api: deps.API}, settings.Interval, settings.InitialLookback, settings.MaxWindow)
}
