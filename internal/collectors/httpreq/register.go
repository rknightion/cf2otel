package httpreq

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
)

// Register installs HTTP event and sampled-data-correct Groups metric collectors.
func Register(deps collector.Deps) {
	if c := deps.Config.Collector("httpreq.threats"); c.Enabled {
		deps.Registry.RegisterWindow(&threats{base{cfg: deps.Config, api: deps.API}}, c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector("httpreq.transfer"); c.Enabled {
		deps.Registry.RegisterSnapshot(&transfer{base: base{cfg: deps.Config, api: deps.API}, now: time.Now}, c.Interval)
	}
	if c := deps.Config.Collector("httpreq.events"); c.Enabled {
		deps.Registry.RegisterWindow(NewEvents(deps.Config, deps.API, deps.Identity), c.Interval, c.InitialLookback, c.MaxWindow)
	}
	if c := deps.Config.Collector("httpreq.metrics"); c.Enabled {
		deps.Registry.RegisterWindow(NewMetrics(deps.Config, deps.API), c.Interval, c.InitialLookback, c.MaxWindow)
	}
}
