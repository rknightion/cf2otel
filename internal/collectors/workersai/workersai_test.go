package workersai_test

import (
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/workersai"
	"github.com/rknightion/cf2otel/internal/config"
)

func TestEnabledCollectorPublicBoundary(t *testing.T) {
	cfg := config.Default()
	cfg.Collectors["workersai.metrics"] = config.CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	registry := collector.NewRegistry()
	workersai.Register(collector.Deps{Config: &cfg, Registry: registry})
	if len(registry.Entries()) != 1 {
		t.Fatalf("enabled Workers AI dataset registered %d collectors, want 1", len(registry.Entries()))
	}
	if _, ok := registry.Entries()[0].Collector.(collector.WindowCollector); !ok {
		t.Fatal("Workers AI collector must be windowed")
	}
}
