package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
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

// batchRegistrationMeter injects faults only at the SDK instrument boundary.
type batchRegistrationMeter struct {
	metric.Meter
	fault func(string) error
}

func (m *batchRegistrationMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	if m.fault != nil {
		if err := m.fault(name); err != nil {
			return nil, err
		}
	}
	return m.Meter.Float64ObservableGauge(name, opts...)
}

func TestGaugeSnapshotsFailedPublicationAtomic(t *testing.T) {
	for _, scenario := range []string{"cancellation", "registration-error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			defer provider.Shutdown(ctx)
			meter := &batchRegistrationMeter{Meter: provider.Meter("test")}
			e := NewEmitter(meter, lognoop.NewLoggerProvider().Logger("test"), tracenoop.NewTracerProvider().Tracer("test")).(*otelEmitter)
			pair := func(status string) map[string][]GaugePoint {
				return map[string][]GaugePoint{
					semconv.MetricCertificateExpiry: {{Value: 42, Attrs: []Attr{{Key: semconv.AttrCertificateStatus, Value: status}}}},
					semconv.MetricCertificatePack:   {{Value: 1, Attrs: []Attr{{Key: semconv.AttrCertificateStatus, Value: status}}}},
				}
			}
			if err := e.GaugeSnapshots(ctx, time.Hour, pair("active")); err != nil {
				t.Fatal(err)
			}
			before := map[string]time.Time{}
			for name, s := range e.snapshots {
				before[name] = s.expires
			}
			attempt, cancel := context.WithCancel(ctx)
			defer cancel()
			sentinel := errors.New("registration failed")
			meter.fault = func(string) error {
				if scenario == "cancellation" {
					cancel()
					return nil
				}
				return sentinel
			}
			next := pair("pending")
			// An additional valid instrument forces real registration after old ones exist.
			next[semconv.MetricCertificateExpiry+".extra"] = nil
			err := e.GaugeSnapshots(attempt, 2*time.Hour, next)
			want := sentinel
			if scenario == "cancellation" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("publication error=%v, want %v", err, want)
			}
			for name, expiry := range before {
				if !e.snapshots[name].expires.Equal(expiry) {
					t.Fatalf("failed publication refreshed %s TTL", name)
				}
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &data); err != nil {
				t.Fatal(err)
			}
			seen := 0
			for _, sm := range data.ScopeMetrics {
				for _, m := range sm.Metrics {
					for _, dp := range m.Data.(metricdata.Gauge[float64]).DataPoints {
						status, _ := dp.Attributes.Value(semconv.AttrCertificateStatus)
						if status.AsString() != "active" {
							t.Fatalf("failed publication exported %s", status.AsString())
						}
						seen++
					}
				}
			}
			if seen != 2 {
				t.Fatalf("retained points=%d, want 2", seen)
			}
			meter.fault = nil
			if err := e.GaugeSnapshots(ctx, time.Hour, map[string][]GaugePoint{semconv.MetricCertificateExpiry: nil, semconv.MetricCertificatePack: nil}); err != nil {
				t.Fatal(err)
			}
			if err := reader.Collect(ctx, &data); err != nil {
				t.Fatal(err)
			}
			for _, sm := range data.ScopeMetrics {
				for _, m := range sm.Metrics {
					if len(m.Data.(metricdata.Gauge[float64]).DataPoints) != 0 {
						t.Fatal("successful empty batch retained points")
					}
				}
			}
		})
	}
}
