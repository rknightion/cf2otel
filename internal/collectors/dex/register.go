// Package dex exports bounded DEX provider interval averages.
package dex

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Register installs the disabled-by-default DEX snapshot collector.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector(semconv.CollectorNameDEXTests)
	if !cfg.Enabled {
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	deps.Registry.RegisterSnapshot(&testsCollector{api: deps.API, account: deps.Config.Cloudflare.AccountID, window: deps.Config.DEX.ResultWindow, maxTests: deps.Config.DEX.MaxTests, limit: deps.Config.DEX.MaxMetricSeries, interval: interval, admitted: map[series]bool{}}, interval)
}
