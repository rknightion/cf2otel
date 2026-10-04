package dns

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestRegisteredDNSLifetimeCardinalityBudget(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		limit, zones, windows, maxPoints int
	}{
		{"default many zones", 10000, 201, 1, 9999},
		{"unlimited SDK still bounded", 0, 201, 1, 9999},
		// Exercise every prefix of the churn sequence with the same provider and
		// collector alive across its windows, then assert its final OTLP export.
		{"small limit initial window", 4, 1, 1, 3},
		{"small limit second window", 4, 1, 2, 3},
		{"small limit window churn", 4, 1, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warnings bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&warnings, nil)))
			defer slog.SetDefault(previous)
			var mu sync.Mutex
			var latest *metricspb.Metric
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if r.URL.Path == "/v1/metrics" {
					var req collectorpb.ExportMetricsServiceRequest
					if err := proto.Unmarshal(body, &req); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					for _, resource := range req.ResourceMetrics {
						for _, scope := range resource.ScopeMetrics {
							for _, m := range scope.Metrics {
								if m.Name == semconv.MetricDNSQueries {
									latest = m
								}
							}
						}
					}
					mu.Unlock()
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.OTLP.MetricCardinalityLimit = tc.limit
			providerLimit := tc.limit
			if providerLimit == 0 {
				providerLimit = -1
			}
			// FlushCommit flushes logs and traces, not metrics. A fast periodic
			// reader can leave latest pointing at a mid-window snapshot when a
			// wall-clock polling deadline expires. Shutdown below synchronously
			// collects and exports all metrics; don't compete with it every 10ms.
			p, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Interval: time.Hour, CardinalityLimit: providerLimit})
			if err != nil {
				t.Fatal(err)
			}
			shutdown := false
			defer func() {
				if shutdown {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := p.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			api := &fakeAPI{settings: map[string]cfapi.DatasetSettings{}, rows: map[string][]map[string]any{}}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
			var c collector.WindowCollector
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == "dns.metrics" {
					c = entry.Collector.(collector.WindowCollector)
				}
			}
			if c == nil {
				t.Fatal("DNS metrics not registered")
			}
			from := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
			var expected float64
			for window := 0; window < tc.windows; window++ {
				api.zones = nil
				for z := 0; z < tc.zones; z++ {
					id := fmt.Sprintf("invented-zone-%d-%d", window, z)
					api.zones = append(api.zones, cfapi.Zone{ID: id, Name: fmt.Sprintf("zone-%d-%d.example.com", window, z)})
					key := id + "/" + groupsDataset
					api.settings[key] = dnsSettings(6, "count", "dimensions_queryType", "dimensions_responseCode", "dimensions_responseCached", "dimensions_responseStale", "dimensions_protocol")
					for q := 0; q < 13; q++ {
						for _, cached := range []bool{false, true} {
							for _, protocol := range []string{"UDP", "TCP"} {
								api.rows[key] = append(api.rows[key], map[string]any{"count": 2, "dimensions": map[string]any{"queryType": fmt.Sprintf("TYPE%d", q), "responseCode": "NOERROR", "responseCached": cached, "responseStale": false, "protocol": protocol}})
								expected += 2
							}
						}
					}
				}
				to := from.Add(time.Minute)
				if mark, err := c.CollectWindow(context.Background(), from, to, p.Emitter); err != nil || !mark.Equal(to) {
					t.Fatalf("collect mark=%s err=%v", mark, err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := p.FlushCommit(ctx, p.BeginCommit())
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				from = to
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = p.Shutdown(ctx)
			cancel()
			shutdown = true
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			m := latest
			mu.Unlock()
			if m == nil {
				t.Fatal("DNS metric missing")
			}
			if sum := m.GetSum(); sum == nil || sum.AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
				t.Fatal("DNS queries must export a cumulative sum")
			}
			var total, folded float64
			normals := 0
			for _, point := range m.GetSum().DataPoints {
				total += point.GetAsDouble()
				aggregated := false
				for _, attr := range point.Attributes {
					if attr.Key == "otel.metric.overflow" && attr.Value.GetBoolValue() {
						t.Errorf("SDK overflow at limit %d after %d windows", tc.limit, tc.windows)
					}
					if attr.Key == semconv.AttrDNSZone && attr.Value.GetStringValue() == "<aggregated>" {
						aggregated = true
					}
					if attr.Key == semconv.AttrDNSColo {
						t.Error("colo leaked to DNS metric")
					}
				}
				if aggregated {
					if len(point.Attributes) != 1 {
						t.Error("fallback has extra dimensions")
					}
					folded += point.GetAsDouble()
				} else {
					normals++
					if len(point.Attributes) != 6 {
						t.Error("normal attributes changed")
					}
				}
			}
			t.Logf("after %d windows: total=%v want=%v points=%d max=%d folded=%v", tc.windows, total, expected, len(m.GetSum().DataPoints), tc.maxPoints, folded)
			if total != expected || len(m.GetSum().DataPoints) > tc.maxPoints || folded == 0 {
				t.Fatalf("total=%v want=%v points=%d max=%d folded=%v", total, expected, len(m.GetSum().DataPoints), tc.maxPoints, folded)
			}
			if tc.limit == 4 && (normals != 2 || folded != expected-4) {
				t.Fatalf("sticky admission lost: normals=%d folded=%v want=%v", normals, folded, expected-4)
			}
			if got := strings.Count(warnings.String(), "DNS metric series coalesced"); got != 1 || !strings.Contains(warnings.String(), semconv.MetricDNSQueries) {
				t.Fatalf("want one coalescing warning naming DNS instrument, got %s", warnings.String())
			}
		})
	}
}
