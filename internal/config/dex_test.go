package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDEXConfigurationPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dex.yaml")
	if err := os.WriteFile(path, []byte("dex:\n  result_window: 2h\n  max_tests: 20\n  max_metric_series: 9\ncollectors:\n  dex.tests:\n    enabled: true\n    interval: 2m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DEX.ResultWindow != 2*time.Hour || c.DEX.MaxTests != 20 || c.DEX.MaxMetricSeries != 9 || !c.Collector("dex.tests").Enabled || c.Collector("dex.tests").Interval != 2*time.Minute {
		t.Fatalf("DEX YAML settings not applied: %+v / %+v", c.DEX, c.Collector("dex.tests"))
	}
	t.Setenv("CF2OTEL_DEX__RESULT_WINDOW", "168h")
	t.Setenv("CF2OTEL_DEX__MAX_TESTS", "10000")
	t.Setenv("CF2OTEL_DEX__MAX_METRIC_SERIES", "5000")
	t.Setenv("CF2OTEL_COLLECTORS__DEX_TESTS__ENABLED", "false")
	t.Setenv("CF2OTEL_COLLECTORS__DEX_TESTS__INTERVAL", "3m")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DEX.ResultWindow != 168*time.Hour || c.DEX.MaxTests != 10000 || c.DEX.MaxMetricSeries != 5000 || c.Collector("dex.tests").Enabled || c.Collector("dex.tests").Interval != 3*time.Minute {
		t.Fatal("DEX environment must override YAML")
	}
}

func TestDEXValidationBounds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		window        time.Duration
		tests, series int
		bad           string
	}{
		{"minimum", time.Hour, 1, 6, ""},
		{"maximum", 168 * time.Hour, 10000, 5000, ""},
		{"short-window", time.Hour - time.Millisecond, 1000, 500, "dex.result_window"},
		{"long-window", 168*time.Hour + time.Millisecond, 1000, 500, "dex.result_window"},
		{"zero-tests", time.Hour, 0, 500, "dex.max_tests"},
		{"too-many-tests", time.Hour, 10001, 500, "dex.max_tests"},
		{"no-remainder-room", time.Hour, 1000, 5, "dex.max_metric_series"},
		{"too-many-series", time.Hour, 1000, 5001, "dex.max_metric_series"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.DEX = DEXConfig{ResultWindow: tc.window, MaxTests: tc.tests, MaxMetricSeries: tc.series}
			cfg := c.Collector("dex.tests")
			cfg.Enabled = true
			c.Collectors["dex.tests"] = cfg
			text := ""
			if err := c.Validate(); err != nil {
				text = err.Error()
			}
			// Required credentials are deliberately absent; check only the DEX errors.
			if tc.bad != "" && !strings.Contains(text, tc.bad) || tc.bad == "" && strings.Contains(text, "dex.") {
				t.Fatalf("DEX validation must enforce %q, got: %s", tc.bad, text)
			}
			if strings.Contains(text, "dex.tests.max_window") {
				t.Fatal("snapshot must not require a window checkpoint")
			}
		})
	}
}

func TestDEXAttributeDenylistConfiguration(t *testing.T) {
	t.Setenv("CF2OTEL_OTLP__ATTRIBUTE_DENYLIST", "cloudflare.dex.test.name,cloudflare.dex.test.kind")
	if _, err := Load(""); err != nil {
		t.Fatalf("declared DEX attributes must be denyable: %v", err)
	}
}
