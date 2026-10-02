package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This uses the public serialized surface so it also compiles on the pre-seam base.
func TestSeamDefaults(t *testing.T) {
	c := Default()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]map[string]any
	// Collectors have nested values; decode only the two sections under test.
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(b, &sections); err != nil {
		t.Fatal(err)
	}
	fields = make(map[string]map[string]any)
	for _, section := range []string{"http", "otlp"} {
		var values map[string]any
		if err := json.Unmarshal(sections[section], &values); err != nil {
			t.Fatal(err)
		}
		fields[section] = values
	}
	if fields["http"]["request_source"] != "eyeball" {
		t.Fatalf("http.request_source = %v, want eyeball", fields["http"]["request_source"])
	}
	if fields["otlp"]["metric_cardinality_limit"] != float64(10000) {
		t.Fatalf("cardinality default = %v", fields["otlp"]["metric_cardinality_limit"])
	}
	breakdowns, ok := fields["http"]["breakdowns"].([]any)
	if !ok || !reflect.DeepEqual(breakdowns, []any{"status", "origin_status", "country", "protocol", "tls_protocol", "method", "content_type"}) {
		t.Fatalf("default breakdowns = %v", fields["http"]["breakdowns"])
	}
	for name, want := range map[string]CollectorConfig{
		"workers.invocations": {Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour},
		"certs.packs":         {Interval: time.Hour},
		"tunnels.status":      {Interval: time.Minute},
	} {
		if got := c.Collector(name); got != want {
			t.Errorf("%s default = %+v, want %+v", name, got, want)
		}
	}
}

func TestSeamLoadAndValidation(t *testing.T) {
	for _, tc := range []struct{ name, yaml, issue string }{
		{"zero unlimited", "otlp:\n  metric_cardinality_limit: 0\n", ""},
		{"negative limit", "otlp:\n  metric_cardinality_limit: -1\n", "otlp.metric_cardinality_limit"},
		{"all source", "http:\n  request_source: all\n", ""},
		{"invalid source", "http:\n  request_source: internal\n", "http.request_source"},
		{"empty breakdowns", "http:\n  breakdowns: []\n", ""},
		{"all breakdowns", "http:\n  breakdowns: [status, origin_status, country, protocol, tls_protocol, method, content_type]\n", ""},
		{"invalid breakdown", "http:\n  breakdowns: [country, unsupported]\n", "http.breakdowns"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatalf("load frozen config: %v", err)
			}
			switch tc.name {
			case "zero unlimited":
				if c.OTLP.MetricCardinalityLimit != 0 {
					t.Fatalf("YAML metric_cardinality_limit = %d, want 0 (unlimited)", c.OTLP.MetricCardinalityLimit)
				}
			case "all source":
				if c.HTTP.RequestSource != "all" {
					t.Fatalf("YAML request_source = %q, want all", c.HTTP.RequestSource)
				}
			case "empty breakdowns":
				if len(c.HTTP.Breakdowns) != 0 {
					t.Fatalf("YAML breakdowns = %v, want empty", c.HTTP.Breakdowns)
				}
			}
			c.Cloudflare.APIToken = "invented-token"
			c.Cloudflare.AccountID = "invented-account"
			c.OTLP.Endpoint = "http://example.com/otlp"
			c.OTLP.GrafanaCloud.InstanceID = "invented-instance"
			c.OTLP.GrafanaCloud.Token = "invented-token"
			err = c.Validate()
			if tc.issue == "" && err != nil {
				t.Fatal(err)
			}
			if tc.issue != "" && (err == nil || !strings.Contains(err.Error(), tc.issue)) {
				t.Fatalf("want %s rejection, got %v", tc.issue, err)
			}
		})
	}
}
