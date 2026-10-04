package config

import (
	"encoding/json"
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
	checkRateLimit(t, c, 1.99, 1)
	if c.Cloudflare.RateLimit.GraphQLRequestsPerSecond != 0.49 || c.Cloudflare.RateLimit.GraphQLBurst != 1 {
		t.Fatal(c.Cloudflare.RateLimit)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    requests_per_second: 1.5\n    burst: 3\n    graphql_requests_per_second: 0.4\n    graphql_burst: 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 1.5, 3)
	if c.Cloudflare.RateLimit.GraphQLRequestsPerSecond != 0.4 || c.Cloudflare.RateLimit.GraphQLBurst != 2 {
		t.Fatal(c.Cloudflare.RateLimit)
	}
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__REQUESTS_PER_SECOND", "1.7")
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__BURST", "2")
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__GRAPHQL_REQUESTS_PER_SECOND", "0.45")
	t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__GRAPHQL_BURST", "1")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 1.7, 2)
	if c.Cloudflare.RateLimit.GraphQLRequestsPerSecond != 0.45 || c.Cloudflare.RateLimit.GraphQLBurst != 1 {
		t.Fatal(c.Cloudflare.RateLimit)
	}
	// Effective environment supersedes invalid YAML scalars for both bursts.
	if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: true\n    graphql_burst: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRateLimit(t, c, 1.7, 2)
}
func TestRateLimitLoadIntegerBurst(t *testing.T) {
	for _, key := range []string{"burst", "graphql_burst"} {
		for _, value := range []string{"true", "1.9", "1000.00000000000001", "1.0000000000000001", "0.99999999999999999", ".inf", "9223372036854775808", "18446744073709551616", "[]", "0", "-1", "1001"} {
			t.Run(key+"_YAML_"+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    "+key+": "+value+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit."+key) {
					t.Fatalf("noninteger burst %s accepted or unrelated error: %v", value, err)
				}
			})
		}
		for _, value := range []string{"true", "1.9", "1.0", "9223372036854775808", "", "0", "-1", "1001"} {
			t.Run(key+"_ENV_"+value, func(t *testing.T) {
				t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__"+strings.ToUpper(key), value)
				if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit."+key) {
					t.Fatalf("noninteger environment burst %q accepted or unrelated error: %v", value, err)
				}
			})
		}
	}
	// Integral spelling remains valid. Legal low rates make capacities explicit;
	// production quota validation intentionally rejects the former burst1000.
	for _, value := range []string{"1", "2", "2.0", "400"} {
		t.Run("valid_"+value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    requests_per_second: 0.5\n    burst: "+value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			expected := value
			if value == "2.0" {
				expected = "2"
			}
			if strconv.Itoa(c.Cloudflare.RateLimit.Burst) != expected {
				t.Fatalf("burst=%d; want %s", c.Cloudflare.RateLimit.Burst, expected)
			}
		})
	}
}
func TestRateLimitLoadRawYAMLSemantics(t *testing.T) {
	for _, key := range []string{"burst", "graphql_burst"} {
		for _, tc := range []struct {
			name, document string
			want           int
		}{
			{"alias_fraction", "cloudflare:\n  rate_limit:\n    requests_per_second: &number 1.0000000000000001\n    BURST: *number\n", 0},
			{"merged_fraction", "cloudflare:\n  rate_limit:\n    <<: {BURST: 0.99999999999999999}\n", 0},
			{"alias_integral", "cloudflare:\n  rate_limit:\n    graphql_requests_per_second: &number 0.4\n    requests_per_second: 1.0\n    BURST: &integer 2.0\n    burst: *integer\n", 2},
			{"explicit_over_merge", "cloudflare:\n  rate_limit:\n    <<: {BURST: 1.0000000000000001}\n    BURST: 2.0\n", 2},
			{"merge_sequence_first_wins", "cloudflare:\n  rate_limit:\n    <<: [{BURST: 2.0}, {BURST: 1.0000000000000001}]\n", 2},
			{"exponent_integral", "cloudflare:\n  rate_limit: {BURST: 20e-1}\n", 2},
			{"octal_integral", "cloudflare:\n  rate_limit: {BURST: 02}\n", 2},
		} {
			t.Run(key+"_"+tc.name, func(t *testing.T) {
				document := strings.ReplaceAll(tc.document, "BURST", key)
				// The integral alias case uses a separate declaration when testing burst
				// itself to avoid duplicate YAML keys, retaining real alias resolution.
				if key == "burst" && tc.name == "alias_integral" {
					document = "cloudflare:\n  rate_limit:\n    requests_per_second: &number 1.0\n    burst: *number\n"
					tc.want = 1
				}
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte(document), 0600); err != nil {
					t.Fatal(err)
				}
				c, err := Load(path)
				if tc.want == 0 {
					if err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit."+key) {
						t.Fatalf("raw fraction accepted or unrelated error: %v", err)
					}
					t.Setenv("CF2OTEL_CLOUDFLARE__RATE_LIMIT__"+strings.ToUpper(key), "3")
					c, err = Load(path)
					if err != nil {
						t.Fatalf("environment precedence: %v", err)
					}
					got := c.Cloudflare.RateLimit.Burst
					if key == "graphql_burst" {
						got = c.Cloudflare.RateLimit.GraphQLBurst
					}
					if got != 3 {
						t.Fatal(got)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				got := c.Cloudflare.RateLimit.Burst
				if key == "graphql_burst" {
					got = c.Cloudflare.RateLimit.GraphQLBurst
				}
				if got != tc.want {
					t.Fatalf("burst=%d; want %d", got, tc.want)
				}
			})
		}
	}
}
func TestRateLimitValidation(t *testing.T) {
	for _, bad := range []RateLimitConfig{
		{RequestsPerSecond: 0, Burst: 1}, {RequestsPerSecond: -1, Burst: 1}, {RequestsPerSecond: math.NaN(), Burst: 1}, {RequestsPerSecond: math.Inf(1), Burst: 1},
		{RequestsPerSecond: 0.5, Burst: 0}, {RequestsPerSecond: 0.5, Burst: -1}, {RequestsPerSecond: 0.5, Burst: 1001},
		{RequestsPerSecond: 10000, Burst: 1}, {RequestsPerSecond: 2, Burst: 1}, {RequestsPerSecond: 0.4, Burst: 1},
		{RequestsPerSecond: 1, Burst: 1, GraphQLRequestsPerSecond: 0.5, GraphQLBurst: 1},
		{RequestsPerSecond: 1, Burst: 1, GraphQLBurst: 1}, {RequestsPerSecond: 1, Burst: 1, GraphQLRequestsPerSecond: 0.49},
		{RequestsPerSecond: 1, Burst: 1, GraphQLRequestsPerSecond: math.NaN(), GraphQLBurst: 1},
		{RequestsPerSecond: 1, Burst: 1, GraphQLRequestsPerSecond: math.Inf(1), GraphQLBurst: 1},
	} {
		if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit") {
			t.Errorf("invalid limit %+v accepted: %v", bad, err)
		}
	}
	for _, good := range []RateLimitConfig{Default().Cloudflare.RateLimit, {RequestsPerSecond: 0.5, Burst: 1}, {RequestsPerSecond: 1, Burst: 300, GraphQLRequestsPerSecond: 0.25, GraphQLBurst: 75}} {
		if err := good.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, doc := range []string{"cloudflare.rate_limit.burst: 1\n", "cloudflare:\n  rate_limit.burst: 1\n", "cloudflare:\n  rate_limit: {requests_per_second: 2, burst: 1}\n", "cloudflare:\n  rate_limit: {graphql_requests_per_second: 0.5, graphql_burst: 1}\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("unsupported/over-quota YAML accepted: %s", doc)
		}
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
