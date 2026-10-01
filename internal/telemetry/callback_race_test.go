package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// Interpose at the SDK observation boundary as well as the legacy independent
// callback boundary. Aggregation, collection, and HTTP OTLP encoding are real.
type reviewBetweenCallbacksMeter struct {
	metric.Meter
	between func()
}

func (m *reviewBetweenCallbacksMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	cfg := metric.NewFloat64ObservableGaugeConfig(opts...)
	replaced := []metric.Float64ObservableGaugeOption{metric.WithUnit(cfg.Unit()), metric.WithDescription(cfg.Description())}
	for _, callback := range cfg.Callbacks() {
		cb := callback
		replaced = append(replaced, metric.WithFloat64Callback(func(ctx context.Context, observer metric.Float64Observer) error {
			err := cb(ctx, observer)
			if name == semconv.MetricCertificateExpiry && m.between != nil {
				f := m.between
				m.between = nil
				f()
			}
			return err
		}))
	}
	return m.Meter.Float64ObservableGauge(name, replaced...)
}

type betweenObservations struct {
	metric.Observer
	meter *reviewBetweenCallbacksMeter
}

func (o betweenObservations) ObserveFloat64(i metric.Float64Observable, value float64, opts ...metric.ObserveOption) {
	o.Observer.ObserveFloat64(i, value, opts...)
	if o.meter.between != nil {
		f := o.meter.between
		o.meter.between = nil
		f()
	}
}

func (m *reviewBetweenCallbacksMeter) RegisterCallback(f metric.Callback, instruments ...metric.Observable) (metric.Registration, error) {
	return m.Meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		return f(ctx, betweenObservations{Observer: observer, meter: m})
	}, instruments...)
}

func TestReviewBatchExportCoherentAcrossCallbacks(t *testing.T) {
	for _, scenario := range []string{"status-change", "empty", "TTL-boundary"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			defer provider.Shutdown(ctx)
			meter := &reviewBetweenCallbacksMeter{Meter: provider.Meter("review")}
			emitter := NewEmitter(meter, lognoop.NewLoggerProvider().Logger("review"), tracenoop.NewTracerProvider().Tracer("review")).(SnapshotBatchEmitter)
			pair := func(status string) map[string][]GaugePoint {
				return map[string][]GaugePoint{
					semconv.MetricCertificateExpiry: {{Value: 42, Attrs: []Attr{{Key: semconv.AttrCertificateStatus, Value: status}}}},
					semconv.MetricCertificatePack:   {{Value: 1, Attrs: []Attr{{Key: semconv.AttrCertificateStatus, Value: status}}}},
				}
			}
			ttl := time.Hour
			if scenario == "TTL-boundary" {
				ttl = 100 * time.Millisecond
			}
			if err := emitter.GaugeSnapshots(ctx, ttl, pair("active")); err != nil {
				t.Fatal(err)
			}
			published := make(chan error, 1)
			interposed := false
			meter.between = func() {
				interposed = true
				if scenario == "TTL-boundary" {
					time.Sleep(150 * time.Millisecond)
					published <- nil
					return
				}
				next := pair("pending")
				if scenario == "empty" {
					next = map[string][]GaugePoint{semconv.MetricCertificateExpiry: nil, semconv.MetricCertificatePack: nil}
				}
				started := make(chan struct{})
				go func() {
					close(started)
					published <- emitter.GaugeSnapshots(ctx, time.Hour, next)
				}()
				<-started
				// On the legacy callback path publication completes between callbacks;
				// with one callback it is blocked by the collection read lock.
				time.Sleep(10 * time.Millisecond)
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &data); err != nil {
				t.Fatal(err)
			}
			if !interposed {
				t.Fatal("SDK observation interleaving did not execute")
			}
			select {
			case err := <-published:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("publication did not finish after collection")
			}
			var received collectormetrics.ExportMetricsServiceRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if err := proto.Unmarshal(body, &received); err != nil {
					t.Error(err)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			exporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(server.URL+"/v1/metrics"))
			if err != nil {
				t.Fatal(err)
			}
			defer exporter.Shutdown(ctx)
			if err := exporter.Export(ctx, &data); err != nil {
				t.Fatal(err)
			}
			statuses := map[string][]string{}
			for _, rm := range received.ResourceMetrics {
				for _, sm := range rm.ScopeMetrics {
					for _, m := range sm.Metrics {
						if m.Name != semconv.MetricCertificateExpiry && m.Name != semconv.MetricCertificatePack {
							continue
						}
						for _, dp := range m.GetGauge().DataPoints {
							for _, a := range dp.Attributes {
								if a.Key == semconv.AttrCertificateStatus {
									statuses[m.Name] = append(statuses[m.Name], a.Value.GetStringValue())
								}
							}
						}
					}
				}
			}
			expiry, presence := statuses[semconv.MetricCertificateExpiry], statuses[semconv.MetricCertificatePack]
			if len(expiry) != 1 || len(presence) != 1 || expiry[0] != "active" || presence[0] != "active" {
				t.Fatalf("torn OTLP batch: expiry=%v presence=%v; want one coherent generation", expiry, presence)
			}
		})
	}
}
