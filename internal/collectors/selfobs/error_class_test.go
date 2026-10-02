package selfobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Observe scheduler failures through the public Stats callback and real SDK,
// alongside the registered snapshot collector. Wrappers must retain their class,
// and message-shaped unknown errors must never become labels.
func TestRegisteredScrapeErrorClasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"not entitled to field 'fixture'"}]}`))
	}))
	t.Cleanup(server.Close)
	client := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 1024})
	_, denied := client.DatasetSettings(context.Background(), cfapi.ZoneScope, "fixture", "fixture")
	if denied == nil {
		t.Fatal("GraphQL entitlement fixture unexpectedly succeeded")
	}
	cases := []struct {
		name  string
		err   error
		class string
	}{
		{"unentitled-http", denied, "unentitled"},
		{"entitlement-before-auth", errors.Join(&cfapi.HTTPError{Status: 403}, &cfapi.UnentitledError{Field: "fixture"}), "unentitled"},
		{"disabled", &cfapi.UnentitledError{Dataset: "fixture", Disabled: true}, "unentitled"},
		{"unavailable-fields", &cfapi.UnentitledError{Dataset: "fixture"}, "unentitled"},
		{"limited", &cfapi.HTTPError{Status: 429}, "rate_limited"},
		{"unauthorized", &cfapi.HTTPError{Status: 401}, "auth"},
		{"forbidden", &cfapi.HTTPError{Status: 403}, "auth"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"network", &net.DNSError{IsTimeout: true}, "timeout"},
		{"field-limit", &cfapi.FieldLimitError{Dataset: "fixture", Wanted: 3, Limit: 2}, "schema"},
		{"json-syntax", &json.SyntaxError{}, "schema"},
		{"json-type", &json.UnmarshalTypeError{}, "schema"},
		{"retention", &cfapi.RetentionGapError{Dataset: "fixture"}, "other"},
		{"saturation", &cfapi.SaturationError{Dataset: "fixture"}, "other"},
		{"server", &cfapi.HTTPError{Status: 500}, "other"},
		{"unknown", errors.New("rate limited auth timeout schema not entitled to field 'fixture'"), "other"},
		{"canceled", context.Canceled, "other"},
	}
	for _, tc := range cases {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", tc.name, wrapped), func(t *testing.T) {
				ctx := context.Background()
				reader := sdkmetric.NewManualReader()
				provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
				t.Cleanup(func() { _ = provider.Shutdown(ctx) })
				emitter := telemetry.NewEmitter(provider.Meter("test"), nil, nil)
				stats := New(emitter, "test", "test")
				registry := collector.NewRegistry()
				cfg := config.Default()
				Register(collector.Deps{Config: &cfg, Registry: registry, SelfObs: NewCollector(stats)})
				failure := tc.err
				if wrapped {
					failure = fmt.Errorf("outer: %w", failure)
				}
				now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
				for range 2 {
					if err := stats.Poll(ctx, "fixture", time.Second, failure, now); err != nil {
						t.Fatal(err)
					}
				}
				if err := stats.Poll(ctx, "fixture", time.Second, nil, now); err != nil {
					t.Fatal(err)
				}
				run := registry.Entries()[0].Collector.(collector.SnapshotCollector)
				if err := run.Collect(ctx, emitter); err != nil {
					t.Fatal(err)
				}
				var data metricdata.ResourceMetrics
				if err := reader.Collect(ctx, &data); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, scope := range data.ScopeMetrics {
					for _, metric := range scope.Metrics {
						if metric.Name != semconv.MetricScrapeErrors {
							continue
						}
						found = true
						sum, ok := metric.Data.(metricdata.Sum[float64])
						if !ok || !sum.IsMonotonic || metric.Unit != "1" {
							t.Fatalf("scrape errors counter contract: %+v", metric)
						}
						if len(sum.DataPoints) != 2 {
							t.Fatalf("error points: %+v", sum.DataPoints)
						}
						seen := make(map[string]bool)
						for _, point := range sum.DataPoints {
							name, present := point.Attributes.Value(attribute.Key(semconv.AttrCollector))
							if !present || point.Attributes.Len() != 2 || seen[name.AsString()] {
								t.Fatalf("error counter leaked attributes or duplicated collector: %+v", point)
							}
							seen[name.AsString()] = true
							wantClass, wantValue := tc.class, float64(2)
							switch name.AsString() {
							case "fixture":
							case "selfobs":
								wantClass, wantValue = "other", 0
							default:
								t.Fatalf("unexpected error point: %+v", point)
							}
							class, ok := point.Attributes.Value(attribute.Key(semconv.AttrErrorClass))
							if !ok || class.AsString() != wantClass || point.Value != wantValue {
								t.Fatalf("error point = %+v, want class %q value %v", point, wantClass, wantValue)
							}
						}
					}
				}
				if !found {
					t.Fatal("missing scrape errors counter")
				}
			})
		}
	}
}
