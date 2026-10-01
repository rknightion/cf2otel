package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the public loader: new names must be accepted by environment lookup,
// YAML must override defaults, and environment must override YAML.
func TestExtensionCollectorLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("collectors:\n  logpush.failures:\n    enabled: false\n    interval: 9m\n  healthchecks.events:\n    enabled: false\n  httpreq.threats:\n    interval: 2h\n  httpreq.transfer:\n    enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF2OTEL_COLLECTORS__LOGPUSH_FAILURES__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__HEALTHCHECKS_EVENTS__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__HTTPREQ_THREATS__INTERVAL", "1h")
	t.Setenv("CF2OTEL_COLLECTORS__HTTPREQ_TRANSFER__ENABLED", "true")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("new collector names must load: %v", err)
	}
	if got := c.Collector("logpush.failures"); !got.Enabled || got.Interval != 9*time.Minute {
		t.Fatalf("logpush precedence: %+v", got)
	}
	if !c.Collector("healthchecks.events").Enabled || !c.Collector("httpreq.transfer").Enabled || c.Collector("httpreq.threats").Interval != time.Hour {
		t.Fatal("extension environment overrides not applied")
	}
}

func TestExtensionCollectorDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"logpush.failures", "healthchecks.events"} {
		if got := c.Collector(name); got != (CollectorConfig{Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}) {
			t.Errorf("opt-in defaults %s: %+v", name, got)
		}
	}
	if got := c.Collector("httpreq.threats"); got != (CollectorConfig{Enabled: true, Interval: time.Hour, InitialLookback: time.Hour, MaxWindow: time.Hour}) {
		t.Errorf("hourly threat defaults: %+v", got)
	}
	if got := c.Collector("httpreq.transfer"); got != (CollectorConfig{Enabled: true, Interval: time.Hour}) {
		t.Errorf("snapshot defaults: %+v", got)
	}
}

func TestExtensionSnapshotValidation(t *testing.T) {
	c := Default()
	c.Cloudflare.APIToken = "test-token"
	c.Cloudflare.AccountID = "test-account"
	c.OTLP.Endpoint = "https://example.com/otlp"
	c.OTLP.GrafanaCloud.InstanceID = "test-instance"
	c.OTLP.GrafanaCloud.Token = "test-otel-token"
	c.Collectors["httpreq.transfer"] = CollectorConfig{Enabled: true, Interval: time.Hour}
	if err := c.Validate(); err != nil {
		t.Fatalf("snapshot has no window: %v", err)
	}
	c.Collectors["httpreq.transfer"] = CollectorConfig{Enabled: true}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "httpreq.transfer.interval") {
		t.Fatalf("invalid snapshot interval: %v", err)
	}
}
