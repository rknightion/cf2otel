package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLoadPrecedenceAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  account_id: yaml-account\nhttp:\n  scope: all\nfirewall:\n  rule_dimensions: true\n  max_metric_series_per_window: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF2OTEL_FIREWALL__MAX_METRIC_SERIES_PER_WINDOW", "3")
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
	if !c.Firewall.RuleDimensions || c.Firewall.MaxMetricSeriesPerWindow != 3 {
		t.Fatalf("firewall config precedence = %+v", c.Firewall)
	}
	c.Firewall.MaxMetricSeriesPerWindow = 0
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "firewall.max_metric_series_per_window must be positive") {
		t.Fatalf("zero firewall cap was not rejected: %v", err)
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

func TestCollectorEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("collectors:\n  aigateway.coverage:\n    enabled: false\n    interval: 1m\n    initial_lookback: 2m\n    max_window: 3m\n  r2.catalog_data:\n    enabled: true\n    interval: 2m\n    initial_lookback: 4m\n    max_window: 6m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__ENABLED":          "true",
		"CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__INTERVAL":         "10m",
		"CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__INITIAL_LOOKBACK": "45m",
		"CF2OTEL_COLLECTORS__AIGATEWAY_COVERAGE__MAX_WINDOW":       "2h",
		"CF2OTEL_COLLECTORS__R2_CATALOG_DATA__ENABLED":             "false",
		"CF2OTEL_COLLECTORS__R2_CATALOG_DATA__INTERVAL":            "7m",
		"CF2OTEL_COLLECTORS__R2_CATALOG_DATA__INITIAL_LOOKBACK":    "25m",
		"CF2OTEL_COLLECTORS__R2_CATALOG_DATA__MAX_WINDOW":          "90m",
	} {
		t.Setenv(key, value)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Collector("aigateway.coverage"); got != (CollectorConfig{Enabled: true, Interval: 10 * time.Minute, InitialLookback: 45 * time.Minute, MaxWindow: 2 * time.Hour}) {
		t.Fatalf("coverage override = %+v", got)
	}
	if got := c.Collector("r2.catalog_data"); got != (CollectorConfig{Enabled: false, Interval: 7 * time.Minute, InitialLookback: 25 * time.Minute, MaxWindow: 90 * time.Minute}) {
		t.Fatalf("underscore-name override = %+v", got)
	}
}

func TestUnknownCollectorEnvironmentName(t *testing.T) {
	key := "CF2OTEL_COLLECTORS__NOT_A_COLLECTOR__ENABLED"
	t.Setenv(key, "true")
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("expected %s in load error, got %v", key, err)
	}
}

func TestDefaultCollectorNamesHaveDistinctEnvironmentForms(t *testing.T) {
	seen := map[string]string{}
	for name := range Default().Collectors {
		form := strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
		if previous, ok := seen[form]; ok {
			t.Errorf("%s and %s share environment form %s", previous, name, form)
		}
		seen[form] = name
	}
}

func TestDefaultEnabledCollectorsUnchanged(t *testing.T) {
	const want = "access.login_metrics,access.logins,access.scim,aigateway.logs,audit.logs,d1.analytics,d1.queries,d1.storage,dns.events,dns.metrics,durableobjects.invocations,durableobjects.periodic,durableobjects.sql_storage,durableobjects.subrequests,email.routing,email.sending,firewall.events,firewall.metrics,gateway.dns,httpreq.events,httpreq.metrics,httpreq.threats,httpreq.transfer,inventory.access,kv.operations,kv.storage,logpush.health,queues.backlog,queues.consumer,queues.delayed_backlog,queues.message_operations,r2.bandwidth,r2.catalog_data,r2.catalog_maintenance,r2.operations,r2.sql,r2.storage,rum.pageloads,rum.web_vitals,selfobs,turnstile.events,workers.invocations,workers.overview"
	var enabled []string
	for name, cfg := range Default().Collectors {
		if cfg.Enabled {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)
	if got := strings.Join(enabled, ","); got != want {
		t.Fatalf("enabled defaults changed:\n%s", got)
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
