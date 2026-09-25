package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrecedenceAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  account_id: yaml-account\nhttp:\n  scope: all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF2OTEL_CLOUDFLARE__ACCOUNT_ID", "env-account")
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "private-token")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "otel-token")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Cloudflare.AccountID != "env-account" || c.HTTP.Scope != "all" || c.Cloudflare.APIToken.Value() != "private-token" {
		t.Fatalf("wrong precedence: account=%q scope=%q token-present=%t", c.Cloudflare.AccountID, c.HTTP.Scope, c.Cloudflare.APIToken != "")
	}
	dump := c.String()
	if strings.Contains(dump, "private-token") || strings.Contains(dump, "otel-token") || !strings.Contains(dump, "[REDACTED]") {
		t.Fatal("secret leaked or redaction absent")
	}
	c.OTLP.Headers["X-Custom"] = "potential-secret"
	if strings.Contains(c.String(), "potential-secret") {
		t.Fatal("header value leaked")
	}
}
func TestYAMLSecretRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  api_token: forbidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("expected env-only error, got %v", err)
	}
}
func TestValidationCollectsErrors(t *testing.T) {
	c := Default()
	c.Cloudflare.Timeout = 0
	c.Identity.MaxCandidates = 0
	c.OTLP.Protocol = "bad"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, part := range []string{"cloudflare.timeout", "identity.max_candidates", "otlp.protocol"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("missing %s: %v", part, err)
		}
	}
}

func TestHTTPMetricControls(t *testing.T) {
	t.Setenv("CF2OTEL_CLOUDFLARE__ACCOUNT_ID", "test-account")
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "test-token")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__INSTANCE_ID", "test-instance")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "test-otel-token")
	t.Setenv("CF2OTEL_HTTP__METRICS_SCOPE", "all")
	t.Setenv("CF2OTEL_HTTP__MAX_METRIC_HOSTS_PER_ZONE", "25")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HTTP.Scope != "access_protected" || loaded.HTTP.MetricsScope != "all" || loaded.HTTP.MaxMetricHostsPerZone != 25 || loaded.HTTP.MaxMetricSeriesPerWindow != 10000 {
		t.Fatalf("HTTP metric controls did not load: %+v", loaded.HTTP)
	}
	loaded.HTTP.MetricsScope = "invalid"
	loaded.HTTP.MaxMetricHostsPerZone = 0
	loaded.HTTP.MaxMetricSeriesPerWindow = 0
	err = loaded.Validate()
	if err == nil {
		t.Fatal("expected invalid HTTP metric controls")
	}
	for _, name := range []string{"http.metrics_scope", "http.max_metric_hosts_per_zone", "http.max_metric_series_per_window"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("validation missed %s: %v", name, err)
		}
	}
}

func TestPlatformMetricSeriesCap(t *testing.T) {
	if got := Default().Platform.MaxMetricSeriesPerWindow; got != 500 {
		t.Fatalf("default platform series cap = %d, want 500", got)
	}
	t.Setenv("CF2OTEL_PLATFORM__MAX_METRIC_SERIES_PER_WINDOW", "25")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Platform.MaxMetricSeriesPerWindow != 25 {
		t.Fatalf("environment platform series cap = %d, want 25", loaded.Platform.MaxMetricSeriesPerWindow)
	}
	loaded.Platform.MaxMetricSeriesPerWindow = 0
	if err := loaded.Validate(); err == nil || !strings.Contains(err.Error(), "platform.max_metric_series_per_window") {
		t.Fatalf("expected platform cap validation error, got %v", err)
	}
}
