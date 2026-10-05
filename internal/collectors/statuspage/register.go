// Package statuspage collects the public vendor status API without credentials.
package statuspage

import (
	"net/http"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Register installs independently opt-in components and incidents collectors.
// This client is deliberately separate from cfapi: no tenant token or headers
// enter this package. The standard transport is safe for concurrent requests.
func Register(deps collector.Deps) {
	components := deps.Config.Collector(semconv.CollectorNameStatuspageComponents)
	incidents := deps.Config.Collector(semconv.CollectorNameStatuspageIncidents)
	if !components.Enabled && !incidents.Enabled {
		return
	}
	cfg := deps.Config.Statuspage
	source := &source{config: cfg, client: &http.Client{Timeout: cfg.Timeout,
		// A fixed API request must not turn into an HTML/login or third-party poll.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if components.Enabled {
		interval := components.Interval
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		deps.Registry.RegisterSnapshot(&componentCollector{source: source, interval: interval}, interval)
	}
	if incidents.Enabled {
		deps.Registry.RegisterWindow(&incidentCollector{source: source}, incidents.Interval, incidents.InitialLookback, incidents.MaxWindow)
	}
}
