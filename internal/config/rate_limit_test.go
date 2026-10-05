package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRateLimitLoadDefaultsAndPrecedence(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimitDefaults(t, c)
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
	// Validate the effective value, not a YAML value superseded by environment.
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 4, 2)
}

func TestRateLimitLoadIntegerBurst(t *testing.T) {
	for _, value := range []string{"true", "1.9", "1000.00000000000001", "1.0000000000000001", "0.99999999999999999", ".inf", "9223372036854775808", "18446744073709551616", "[]", "0", "-1", "1001"} {
		t.Run("YAML_"+value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: "+value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit.") {
				t.Fatalf("noninteger burst %s accepted or unrelated error: %v", value, err)
			}
		})
	}
	for _, value := range []string{"true", "1.9", "1.0", "9223372036854775808", "", "0", "-1", "1001"} {
		t.Run("ENV_"+value, func(t *testing.T) {
			t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__BURST", value)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit.") {
				t.Fatalf("noninteger environment burst %q accepted or unrelated error: %v", value, err)
			}
		})
	}
	// Numeric YAML spelling is immaterial; exact integral values in [1,1000] work.
	for _, value := range []string{"1", "2", "2.0", "1000"} {
		t.Run("valid_"+value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: "+value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if value == "2.0" {
				if c.Cloudflare.RateLimit.REST.Burst != 2 {
					t.Fatalf("burst=%d; want 2", c.Cloudflare.RateLimit.REST.Burst)
				}
			} else if strconv.Itoa(c.Cloudflare.RateLimit.REST.Burst) != value {
				t.Fatalf("burst=%d; want %s", c.Cloudflare.RateLimit.REST.Burst, value)
			}
		})
	}
}

func TestRateLimitLoadRawYAMLSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, document string
		want           int
	}{
		{"alias_fraction", "cloudflare:\n  rate_limit:\n    requests_per_second: &number 1.0000000000000001\n    burst: *number\n", 0},
		{"merged_fraction", "cloudflare:\n  rate_limit:\n    <<: {burst: 0.99999999999999999}\n", 0},
		{"alias_integral", "cloudflare:\n  rate_limit:\n    requests_per_second: &number 2.0\n    burst: *number\n", 2},
		{"explicit_over_merge", "cloudflare:\n  rate_limit:\n    <<: {burst: 1.0000000000000001}\n    burst: 2.0\n", 2},
		{"merge_sequence_first_wins", "cloudflare:\n  rate_limit:\n    <<: [{burst: 2.0}, {burst: 1.0000000000000001}]\n", 2},
		{"exponent_integral", "cloudflare:\n  rate_limit: {burst: 20e-1}\n", 2},
		{"octal_integral", "cloudflare:\n  rate_limit: {burst: 02}\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.document), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit.") {
					t.Fatalf("raw fraction accepted or unrelated error: %v", err)
				}
				// The environment supersedes even an invalid aliased/merged scalar.
				t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__BURST", "3")
				c, err = Load(path)
				if err != nil || c.Cloudflare.RateLimit.REST.Burst != 3 {
					t.Fatalf("environment precedence failed: config=%v err=%v", c, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Cloudflare.RateLimit.REST.Burst != tc.want {
				t.Fatalf("burst=%d; want %d", c.Cloudflare.RateLimit.REST.Burst, tc.want)
			}
		})
	}
}

func TestRateLimitValidation(t *testing.T) {
	for _, bad := range []BucketConfig{{0, 1}, {-1, 1}, {math.NaN(), 1}, {math.Inf(1), 1}, {0.5, 0}, {0.5, -1}, {0.5, 1001}} {
		if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit") {
			t.Errorf("invalid limit %+v accepted: %v", bad, err)
		}
	}
	if err := (BucketConfig{0.5, 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func checkRateLimitDefaults(t *testing.T, c *Config) {
	t.Helper()
	rest, graphql := c.Cloudflare.RateLimit.Buckets()
	if rest != (BucketConfig{3, 5}) || graphql != (BucketConfig{0.8, 2}) {
		t.Fatalf("defaults: REST=%+v GraphQL=%+v", rest, graphql)
	}
}

func checkRateLimit(t *testing.T, c *Config, rate, burst float64) {
	t.Helper()
	raw, err := c.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	rest, graphql := got.Cloudflare.RateLimit.Buckets()
	want := BucketConfig{rate, int(burst)}
	if rest != want || graphql != want {
		t.Fatalf("effective REST=%+v GraphQL=%+v; want %+v", rest, graphql, want)
	}
}

func TestPerClassRateLimitCompatibilityAndWarning(t *testing.T) {
	var log bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    requests_per_second: 2.5\n    burst: 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 2.5, 4)
	if strings.Count(log.String(), "WARN") != 1 {
		t.Fatalf("want one deprecation warning: %s", log.String())
	}
	for _, class := range []string{"rest", "graphql"} {
		for _, key := range []string{"requests_per_second", "burst"} {
			if !strings.Contains(log.String(), "cloudflare.rate_limit."+class+"."+key) {
				t.Fatalf("warning omitted new key: %s", log.String())
			}
		}
	}
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__REST__REQUESTS_PER_SECOND", "7")
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__GRAPHQL__BURST", "3")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rest, graphql := c.Cloudflare.RateLimit.Buckets()
	if rest != (BucketConfig{7, 4}) || graphql != (BucketConfig{2.5, 3}) {
		t.Fatalf("nested env precedence: REST=%+v GraphQL=%+v", rest, graphql)
	}
}

func TestPerClassRateLimitNestedValidation(t *testing.T) {
	for _, class := range []string{"rest", "graphql"} {
		for _, value := range []string{"true", "1.9", "1.0000000000000001", ".inf", "0", "1001"} {
			t.Run(class+"/burst/"+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    "+class+":\n      burst: "+value+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit."+class) {
					t.Fatalf("invalid nested burst accepted: %v", err)
				}
				t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__"+strings.ToUpper(class)+"__BURST", "2")
				if _, err := Load(path); err != nil {
					t.Fatalf("nested env did not supersede invalid YAML: %v", err)
				}
			})
		}
		for _, value := range []string{"0", "-1", "NaN", "+Inf"} {
			t.Run(class+"/rate/"+value, func(t *testing.T) {
				t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__"+strings.ToUpper(class)+"__REQUESTS_PER_SECOND", value)
				if _, err := Load(""); err == nil {
					t.Fatal("invalid rate accepted")
				}
			})
		}
	}
}

func TestPerClassLegacyPartialAndSourcePrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: 4\n    rest:\n      requests_per_second: 6\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rest, graphql := c.Cloudflare.RateLimit.Buckets()
	if rest != (BucketConfig{6, 4}) || graphql != (BucketConfig{0.8, 4}) {
		t.Fatalf("partial legacy lost defaults: REST=%+v GraphQL=%+v", rest, graphql)
	}
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__REQUESTS_PER_SECOND", "8")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rest, graphql = c.Cloudflare.RateLimit.Buckets()
	if rest != (BucketConfig{8, 4}) || graphql != (BucketConfig{8, 4}) {
		t.Fatalf("legacy env did not win over nested YAML: REST=%+v GraphQL=%+v", rest, graphql)
	}
}
