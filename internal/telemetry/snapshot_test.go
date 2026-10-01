package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricsproto "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestGaugeSnapshotOTLPReplacement(t *testing.T) {
	for _, scenario := range []string{"status-change", "disappearance", "empty", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			var exported []*metricsproto.NumberDataPoint
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var req collectormetrics.ExportMetricsServiceRequest
				if err := proto.Unmarshal(body, &req); err != nil {
					t.Error(err)
					return
				}
				exported = nil
				for _, rm := range req.ResourceMetrics {
					for _, sm := range rm.ScopeMetrics {
						for _, m := range sm.Metrics {
							if m.Name == semconv.MetricCertificateExpiry {
								exported = append(exported, m.GetGauge().DataPoints...)
							}
						}
					}
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			p, err := NewProviders(ctx, ProviderOptions{Endpoint: server.URL, Interval: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := p.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			e := p.Emitter.(SnapshotEmitter)
			point := func(id, status string) GaugePoint {
				return GaugePoint{Value: 42, Attrs: []Attr{{Key: semconv.AttrCertificatePackID, Value: id}, {Key: semconv.AttrCertificateStatus, Value: status}}}
			}
			ttl := time.Hour
			if err := e.GaugeSnapshot(ctx, semconv.MetricCertificateExpiry, ttl, []GaugePoint{point("first", "active"), point("second", "active")}); err != nil {
				t.Fatal(err)
			}
			flush := func() {
				t.Helper()
				exported = nil
				if err := p.metrics.ForceFlush(ctx); err != nil {
					t.Fatal(err)
				}
			}
			flush()
			if len(exported) != 2 {
				t.Fatalf("initial snapshot points=%d, want 2", len(exported))
			}
			var next []GaugePoint
			want := 0
			switch scenario {
			case "status-change":
				next = []GaugePoint{point("first", "pending"), point("second", "pending")}
				want = 2
			case "disappearance":
				next = []GaugePoint{point("first", "active")}
				want = 1
			case "stale":
				// Publish the short-lived snapshot only after proving the initial
				// export; slow HTTP/SDK startup cannot race that first assertion.
				if err := e.GaugeSnapshot(ctx, semconv.MetricCertificateExpiry, time.Millisecond, []GaugePoint{point("first", "active")}); err != nil {
					t.Fatal(err)
				}
				time.Sleep(5 * time.Millisecond)
			}
			if scenario != "stale" {
				if err := e.GaugeSnapshot(ctx, semconv.MetricCertificateExpiry, ttl, next); err != nil {
					t.Fatal(err)
				}
			}
			flush()
			if len(exported) != want {
				t.Fatalf("%s exported points=%d, want %d (obsolete snapshot leaked)", scenario, len(exported), want)
			}
			if scenario == "status-change" {
				for _, dp := range exported {
					for _, a := range dp.Attributes {
						if a.Key == semconv.AttrCertificateStatus && a.Value.GetStringValue() != "pending" {
							t.Fatalf("obsolete status exported: %s", a.Value.GetStringValue())
						}
					}
				}
			}
		})
	}
}
