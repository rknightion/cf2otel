package selfobs

import (
	"context"
	"log/slog"

	"github.com/rknightion/cf2otel/internal/collector"
)

// Register installs exporter self-observability.
func Register(deps collector.Deps) {
	if deps.SelfObs == nil {
		return
	}
	if c, ok := deps.SelfObs.(*Collector); ok {
		deps.Registry.ObserveRegistrations(func(entry collector.Entry) {
			_, windowed := entry.Collector.(collector.WindowCollector)
			if err := c.Stats.Register(context.Background(), entry.Collector.Name(), windowed); err != nil {
				slog.Error("self-observability initialization failed", "collector", entry.Collector.Name(), "error", err)
			}
		})
	}
	if c := deps.Config.Collector("selfobs"); c.Enabled {
		deps.Registry.RegisterSnapshot(deps.SelfObs, c.Interval)
	}
}
