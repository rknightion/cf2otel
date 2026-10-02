package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRateLimitLoadDefaultsAndPrecedence(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 0.5, 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    requests_per_second: 2.5\n    burst: 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 2.5, 3)
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__REQUESTS_PER_SECOND", "4")
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__BURST", "2")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 4, 2)
}

func TestRateLimitValidation(t *testing.T) {
	for _, bad := range []RateLimitConfig{{0, 1}, {-1, 1}, {math.NaN(), 1}, {math.Inf(1), 1}, {0.5, 0}, {0.5, -1}} {
		if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit") {
			t.Errorf("invalid limit %+v accepted: %v", bad, err)
		}
	}
	if err := (RateLimitConfig{0.5, 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func checkRateLimit(t *testing.T, c *Config, rate, burst float64) {
	t.Helper()
	raw, err := c.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Cloudflare struct {
			RateLimit struct {
				RequestsPerSecond float64 `json:"requests_per_second"`
				Burst             float64 `json:"burst"`
			} `json:"rate_limit"`
		} `json:"cloudflare"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cloudflare.RateLimit.RequestsPerSecond != rate || got.Cloudflare.RateLimit.Burst != burst {
		t.Fatalf("effective limiter=%+v; want rate=%v burst=%v", got.Cloudflare.RateLimit, rate, burst)
	}
}
