// Package loadbalancers exports documented provider health flags.
package loadbalancers

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Register installs the disabled-by-default provider health snapshot collector.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector(semconv.CollectorNameLBHealth)
	if !cfg.Enabled {
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	deps.Registry.RegisterSnapshot(&healthCollector{api: deps.API, account: deps.Config.Cloudflare.AccountID, interval: interval, limit: deps.Config.Platform.MaxMetricSeriesPerWindow, admitted: map[string]bool{}}, interval)
}
