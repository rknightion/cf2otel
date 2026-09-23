package aigateway

import (
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
)

func TestRegisterCapsWindowForBoundedExport(t *testing.T) {
	for _, tc := range []struct {
		configured, want time.Duration
	}{
		{3 * time.Hour, 15 * time.Minute},
		{5 * time.Minute, 5 * time.Minute},
	} {
		cfg := &config.Config{Collectors: map[string]config.CollectorConfig{
			"aigateway.logs": {Enabled: true, Interval: time.Minute, InitialLookback: 3 * time.Hour, MaxWindow: tc.configured},
		}}
		registry := collector.NewRegistry()
		Register(collector.Deps{Config: cfg, Registry: registry})
		entries := registry.Entries()
		if len(entries) != 1 || entries[0].MaxWindow != tc.want || entries[0].InitialLookback != 3*time.Hour {
			t.Fatalf("configured %s: entries = %#v, want window %s and preserved lookback", tc.configured, entries, tc.want)
		}
	}
}
