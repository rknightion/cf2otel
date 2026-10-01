package certs

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
)

// Register installs the opt-in certificate-pack snapshot collector.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector("certs.packs")
	if cfg.Enabled {
		deps.Registry.RegisterSnapshot(&packs{api: deps.API, interval: cfg.Interval, warned: make(map[string]time.Time)}, cfg.Interval)
	}
}
