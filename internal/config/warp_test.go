package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWARPYAMLKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte("warp:\n  last_seen_window: 12m\n  max_metric_series: 7\ncollectors:\n  warp.fleet:\n    enabled: true\n    interval: 2m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Collector("warp.fleet").Enabled {
		t.Fatal("WARP not enabled from YAML")
	}
	if c.WARP.LastSeenWindow != 12*time.Minute || c.WARP.MaxMetricSeries != 7 {
		t.Fatalf("WARP keys not loaded: %+v", c.WARP)
	}
}

func TestWARPDefaultsEnvironmentAndBounds(t *testing.T) {
	d := Default()
	if d.Collector("warp.fleet").Enabled || d.Collector("warp.fleet").Interval != 5*time.Minute || d.WARP.LastSeenWindow != 15*time.Minute || d.WARP.MaxMetricSeries != 500 {
		t.Fatal("incorrect WARP defaults")
	}
	t.Setenv("CF2OTEL_WARP__LAST_SEEN_WINDOW", "60m")
	t.Setenv("CF2OTEL_WARP__MAX_METRIC_SERIES", "5000")
	t.Setenv("CF2OTEL_COLLECTORS__WARP_FLEET__ENABLED", "true")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.WARP.LastSeenWindow != time.Hour || c.WARP.MaxMetricSeries != 5000 || !c.Collector("warp.fleet").Enabled {
		t.Fatal("WARP environment precedence")
	}
	for _, tc := range []struct {
		window time.Duration
		limit  int
		bad    bool
	}{{time.Minute, 1, false}, {time.Hour, 5000, false}, {0, 500, true}, {time.Hour + time.Second, 500, true}, {time.Minute, 0, true}, {time.Minute, 5001, true}} {
		c.WARP.LastSeenWindow = tc.window
		c.WARP.MaxMetricSeries = tc.limit
		// Unrelated required credentials remain absent; inspect only our owned validation.
		err := c.Validate()
		text := ""
		if err != nil {
			text = err.Error()
		}
		if strings.Contains(text, "warp.") != tc.bad {
			t.Fatalf("WARP validation %v/%d: %s", tc.window, tc.limit, text)
		}
		if strings.Contains(text, "warp.fleet.max_window") {
			t.Fatal("snapshot incorrectly requires windows")
		}
	}
}
