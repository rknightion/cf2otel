package certs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricsproto "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

type snapshotRecorder struct {
	recordingEmitter
	calls map[string][]telemetry.GaugePoint
	ttl   time.Duration
}

func (e *snapshotRecorder) GaugeSnapshot(_ context.Context, name string, ttl time.Duration, points []telemetry.GaugePoint) error {
	if e.calls == nil {
		e.calls = make(map[string][]telemetry.GaugePoint)
	}
	e.calls[name] = points
	e.ttl = ttl
	return nil
}

// TestRegisterSnapshotOTLP exercises discovery, REST reads, registration, the SDK
// callback and the real OTLP HTTP exporter as one public-boundary path.
func TestRegisterSnapshotOTLP(t *testing.T) {
	ctx := context.Background()
	mode := "active"
	c := snapshot(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			if mode == "no-zones" {
				_, _ = fmt.Fprint(w, `{"result":[]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"result":[{"id":"first","name":"first"},{"id":"second","name":"second"}]}`)
			return
		}
		if mode == "failed" && r.URL.Path == "/zones/second/ssl/certificate_packs" {
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, `{"errors":[{"code":9109}]}`)
			return
		}
		if mode == "empty" || (mode == "removed" && r.URL.Path == "/zones/second/ssl/certificate_packs") {
			_, _ = fmt.Fprint(w, `{"result":[]}`)
			return
		}
		status := mode
		if mode == "removed" || mode == "failed" {
			status = "pending"
		}
		_, _ = fmt.Fprintf(w, `{"result":[{"id":"first","status":%q,"certificates":[{"expires_on":"2040-01-01T00:00:00Z"}]},{"id":"unknown","status":%q,"certificates":[]}]}`, status, status)
	}, nil)
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
	defer func() {
		if err := exporter.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	emitter := telemetry.NewEmitter(provider.Meter("test"), lognoop.NewLoggerProvider().Logger("test"), tracenoop.NewTracerProvider().Tracer("test"))
	for _, step := range []struct {
		mode             string
		expiry, presence int
	}{
		{"active", 2, 4}, {"pending", 2, 4}, {"failed", 2, 4}, {"removed", 1, 2}, {"empty", 0, 0}, {"active", 2, 4}, {"no-zones", 0, 0},
	} {
		mode = step.mode
		err := c.Collect(ctx, emitter)
		if (err != nil) != (mode == "failed") {
			t.Fatalf("%s collect error=%v", mode, err)
		}
		var data metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &data); err != nil {
			t.Fatal(err)
		}
		received.Reset()
		if err := exporter.Export(ctx, &data); err != nil {
			t.Fatal(err)
		}
		points := make(map[string][]*metricsproto.NumberDataPoint)
		for _, rm := range received.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					points[m.Name] = m.GetGauge().DataPoints
				}
			}
		}
		if len(points[semconv.MetricCertificateExpiry]) != step.expiry || len(points[semconv.MetricCertificatePack]) != step.presence {
			t.Fatalf("%s OTLP expiry=%d presence=%d, want %d/%d", mode, len(points[semconv.MetricCertificateExpiry]), len(points[semconv.MetricCertificatePack]), step.expiry, step.presence)
		}
		for _, rows := range points {
			for _, dp := range rows {
				for _, a := range dp.Attributes {
					if a.Key == semconv.AttrCertificateStatus {
						want := mode
						if mode == "failed" || mode == "removed" {
							want = "pending"
						}
						if a.Value.GetStringValue() != want {
							t.Fatalf("%s obsolete status=%s, want %s", mode, a.Value.GetStringValue(), want)
						}
					}
				}
			}
		}
	}
}

func TestRegisterCompletePackSnapshot(t *testing.T) {
	for _, mode := range []string{"complete", "empty", "partial-failure"} {
		t.Run(mode, func(t *testing.T) {
			c := snapshot(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/zones" {
					_, _ = fmt.Fprint(w, `{"result":[{"id":"first","name":"first"},{"id":"second","name":"second"}]}`)
					return
				}
				if mode == "partial-failure" && r.URL.Path == "/zones/second/ssl/certificate_packs" {
					w.WriteHeader(403)
					_, _ = fmt.Fprint(w, `{"errors":[{"code":9109}]}`)
					return
				}
				if mode == "empty" {
					_, _ = fmt.Fprint(w, `{"result":[]}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"result":[{"id":"first","status":"pending","certificates":[{"expires_on":"bad"}]}]}`)
			}, nil)
			e := &snapshotRecorder{}
			err := c.Collect(context.Background(), e)
			if mode == "partial-failure" {
				if err == nil {
					t.Fatal("failed zone must fail entire snapshot")
				}
				if len(e.calls) != 0 || len(e.points) != 0 {
					t.Fatal("partial snapshot published")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(e.calls) != 2 {
				t.Fatalf("snapshot calls=%d, want both gauges", len(e.calls))
			}
			want := 2
			if mode == "empty" {
				want = 0
			}
			if len(e.calls[semconv.MetricCertificateExpiry]) != 0 || len(e.calls["cloudflare.certificate.pack"]) != want {
				t.Fatalf("unknown-expiry presence snapshot=%v", e.calls)
			}
			if e.ttl != 3*time.Hour {
				t.Fatalf("TTL=%s, want three intervals", e.ttl)
			}
			for _, p := range e.calls["cloudflare.certificate.pack"] {
				if p.Value != 1 {
					t.Fatalf("presence=%v, want 1", p.Value)
				}
			}
		})
	}
}
