package config

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStatuspageValidation(t *testing.T) {
	good := Default().Statuspage
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"relative", "ftp://opaque", "https://opaque/?q=1", "https://opaque/#fragment", "https://opaque/#", "https://opaque/?", "https://opaque@opaque/"} {
		t.Run(url, func(t *testing.T) {
			c := good
			c.BaseURL = url
			if err := c.Validate(); err == nil {
				t.Fatal("ambiguous or unsafe origin accepted")
			}
		})
	}
	for _, cap := range []int{0, 5001} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			c := good
			c.ComponentCap = cap
			if err := c.Validate(); err == nil {
				t.Fatal("unbounded component cap accepted")
			}
		})
	}
	c := good
	c.Timeout = 0
	if err := c.Validate(); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
	c = good
	c.MaxResponseBytes = 0
	if err := c.Validate(); err == nil {
		t.Fatal("unbounded response accepted")
	}
}

// The shared scheduler truncates retained checkpoints to seconds. Fractional
// startup/window bounds can otherwise move a committed incident cursor backward.
func TestStatuspageWholeSecondWindowValidation(t *testing.T) {
	valid := func() Config {
		c := Default()
		c.Cloudflare.APIToken = "opaque-token"
		c.Cloudflare.AccountID = "opaque-account"
		c.OTLP.Endpoint = "https://opaque.invalid/otlp"
		c.OTLP.GrafanaCloud.InstanceID = "opaque-instance"
		c.OTLP.GrafanaCloud.Token = "opaque-token"
		return c
	}
	for _, name := range []string{"statuspage.components", "statuspage.incidents"} {
		for _, enabled := range []bool{false, true} {
			for _, field := range []string{"initial_lookback", "max_window"} {
				for _, fraction := range []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, time.Second + time.Nanosecond} {
					t.Run(fmt.Sprintf("%s/enabled=%v/%s/%s", name, enabled, field, fraction), func(t *testing.T) {
						c := valid()
						entry := CollectorConfig{Enabled: enabled, Interval: time.Second, InitialLookback: time.Second, MaxWindow: time.Second}
						if field == "initial_lookback" {
							entry.InitialLookback = fraction
						} else {
							entry.MaxWindow = fraction
						}
						c.Collectors[name] = entry
						if err := c.Validate(); err == nil || !strings.Contains(err.Error(), name+"."+field+" must be a whole number of seconds") {
							t.Fatalf("fractional statuspage checkpoint configuration accepted: %v", err)
						}
					})
				}
			}
			for _, lookback := range []time.Duration{0, time.Second, 15 * time.Minute} {
				c := valid()
				c.Collectors[name] = CollectorConfig{Enabled: enabled, Interval: 500 * time.Millisecond, InitialLookback: lookback, MaxWindow: time.Second}
				if err := c.Validate(); err != nil {
					t.Fatalf("whole-second bounds or fractional polling interval rejected: %v", err)
				}
			}
		}
	}
	// Pin the reported supported-config defect through the environment loader,
	// not only direct struct construction: 1s lookback with a 500ms window.
	t.Setenv("CF2OTEL_COLLECTORS__STATUSPAGE_INCIDENTS__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__STATUSPAGE_INCIDENTS__INITIAL_LOOKBACK", "1s")
	t.Setenv("CF2OTEL_COLLECTORS__STATUSPAGE_INCIDENTS__MAX_WINDOW", "500ms")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Validate(); err == nil || !strings.Contains(err.Error(), "statuspage.incidents.max_window must be a whole number of seconds") {
		t.Fatalf("loaded 1s lookback/500ms window accepted: %v", err)
	}
}

func TestStatuspageDefaultModesAndEnvironment(t *testing.T) {
	c := Default()
	if got := c.Collector("statuspage.components"); got != (CollectorConfig{Interval: 5 * time.Minute}) {
		t.Fatalf("independent off snapshot defaults: %+v", got)
	}
	if got := c.Collector("statuspage.incidents"); got != (CollectorConfig{Interval: 5 * time.Minute, InitialLookback: 15 * time.Minute, MaxWindow: time.Hour}) {
		t.Fatalf("independent off window defaults: %+v", got)
	}
	t.Setenv("CF2OTEL_COLLECTORS__STATUSPAGE_COMPONENTS__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__STATUSPAGE_INCIDENTS__INTERVAL", "9m")
	t.Setenv("CF2OTEL_STATUSPAGE__COMPONENT_CAP", "17")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Collector("statuspage.components").Enabled || loaded.Collector("statuspage.incidents").Enabled || loaded.Collector("statuspage.incidents").Interval != 9*time.Minute {
		t.Fatal("independent environment overrides lost")
	}
	// JSON checks public config encoding without requiring an undeclared type on
	// the red base. Runtime collectors also exercise the loader through YAML.
	b, err := loaded.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"component_cap": 17`) {
		t.Fatal("statuspage environment override absent")
	}
}
