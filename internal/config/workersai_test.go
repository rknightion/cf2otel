package config

import (
	"testing"
	"time"
)

func TestWorkersAIOptInEnvironment(t *testing.T) {
	defaults := Default()
	settings, exists := defaults.Collectors["workersai.metrics"]
	if !exists || settings.Enabled || settings.Interval != 5*time.Minute || settings.InitialLookback != 30*time.Minute || settings.MaxWindow != time.Hour {
		t.Fatalf("Workers AI opt-in window defaults: exists=%v settings=%+v", exists, settings)
	}
	t.Setenv("CF2OTEL_COLLECTORS__WORKERSAI_METRICS__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__WORKERSAI_METRICS__INTERVAL", "10m")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Collector("workersai.metrics").Enabled || cfg.Collector("workersai.metrics").Interval != 10*time.Minute {
		t.Fatal("Workers AI collector environment opt-in ignored")
	}
}
