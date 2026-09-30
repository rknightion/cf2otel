package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSeamEnvironmentPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("otlp:\n  metric_cardinality_limit: 25\nhttp:\n  request_source: eyeball\n  breakdowns: [status]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF2OTEL_OTLP__METRIC_CARDINALITY_LIMIT", "0")
	t.Setenv("CF2OTEL_HTTP__REQUEST_SOURCE", "all")
	t.Setenv("CF2OTEL_HTTP__BREAKDOWNS", "country,method")
	t.Setenv("CF2OTEL_COLLECTORS__WORKERS_INVOCATIONS__INTERVAL", "10m")
	t.Setenv("CF2OTEL_COLLECTORS__CERTS_PACKS__ENABLED", "true")
	t.Setenv("CF2OTEL_COLLECTORS__TUNNELS_STATUS__ENABLED", "true")
	for _, tc := range []struct {
		name, env string
		want      []any
	}{
		{"selected", "country,method", []any{"country", "method"}},
		{"empty", "", []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CF2OTEL_HTTP__BREAKDOWNS", tc.env)
			c, err := Load(path)
			if err != nil {
				t.Fatalf("load new environment surface: %v", err)
			}
			b, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			var http, otlp map[string]any
			if err := json.Unmarshal(fields["http"], &http); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fields["otlp"], &otlp); err != nil {
				t.Fatal(err)
			}
			if http["request_source"] != "all" || otlp["metric_cardinality_limit"] != float64(0) || !reflect.DeepEqual(http["breakdowns"], tc.want) {
				t.Fatalf("environment precedence: source=%v limit=%v breakdowns=%v", http["request_source"], otlp["metric_cardinality_limit"], http["breakdowns"])
			}
			if got := c.Collector("workers.invocations"); !got.Enabled || got.Interval != 10*time.Minute {
				t.Errorf("workers override = %+v", got)
			}
			for _, name := range []string{"certs.packs", "tunnels.status"} {
				if got := c.Collector(name); !got.Enabled || got.MaxWindow != 0 || got.InitialLookback != 0 {
					t.Errorf("snapshot override %s = %+v", name, got)
				}
			}
			c.Cloudflare.APIToken = "invented-token"
			c.Cloudflare.AccountID = "invented-account"
			c.OTLP.Endpoint = "http://example.com/otlp"
			c.OTLP.GrafanaCloud.InstanceID = "invented-instance"
			c.OTLP.GrafanaCloud.Token = "invented-token"
			if err := c.Validate(); err != nil {
				t.Fatalf("enabled snapshots must validate without windows: %v", err)
			}
		})
	}
}

func TestSeamExampleLoads(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The example supplies an endpoint and explicit empty lists instead of nil.
	want := Default()
	if c.OTLP.Endpoint == "" {
		t.Fatal("example must supply an OTLP endpoint")
	}
	want.OTLP.Endpoint = c.OTLP.Endpoint
	want.Cloudflare.Zones = []string{}
	want.HTTP.Hosts = []string{}
	want.HTTP.Zones = []string{}
	want.AIGateway.Gateways = []string{}
	if !reflect.DeepEqual(*c, want) {
		t.Fatalf("example differs from documented defaults: %s", c)
	}
}
