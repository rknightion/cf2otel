package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/rknightion/cf2otel/internal/promexport"
	"github.com/rknightion/cf2otel/internal/semconv"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestPrometheusAlongsideOTLP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var mu sync.Mutex
	var exports []*collectormetric.ExportMetricsServiceRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/v1/metrics" {
			data := new(collectormetric.ExportMetricsServiceRequest)
			if err := proto.Unmarshal(b, data); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			exports = append(exports, data)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer upstream.Close()
	before, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	pull, err := promexport.New()
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProviders(ctx, ProviderOptions{Endpoint: upstream.URL, Interval: time.Hour, PrometheusReader: pull.Reader})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	server, err := pull.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serving, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- server.Run(serving) }()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	url := "http://" + server.Addr().String()
	attrs := []Attr{{Key: semconv.AttrStatusClass, Value: "2xx"}}
	if err := p.Emitter.Counter(ctx, semconv.MetricAPIRequests, 3, attrs...); err != nil {
		t.Fatal(err)
	}
	if err := p.Emitter.Gauge(ctx, semconv.MetricScrapeLastSuccess, 42); err != nil {
		t.Fatal(err)
	}
	if err := p.Emitter.Histogram(ctx, semconv.MetricAPIDuration, .5, attrs...); err != nil {
		t.Fatal(err)
	}
	scrape := func() string {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/metrics", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("scrape status %d: %s", res.StatusCode, b)
		}
		// Explicitly use the legacy metric-name validation scheme for underscore exposition.
		parser := expfmt.NewTextParser(model.LegacyValidation)
		if _, err := parser.TextToMetricFamilies(strings.NewReader(string(b))); err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for range 3 {
		text := scrape()
		for _, want := range []string{"cf2otel_api_requests_total", "cf2otel_scrape_last_success_timestamp_seconds", "cf2otel_api_duration_seconds_bucket", `"2xx"`} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %s in %s", want, text)
			}
		}
		for _, forbidden := range []string{"target_info", "otel_scope", "service_instance", "go_gc", "process_"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("unexpected metadata %s", forbidden)
			}
		}
	}
	if err := p.metrics.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(exports) != 1 {
		t.Fatalf("scrapes triggered OTLP exports: %d", len(exports))
	}
	data := exports[0]
	mu.Unlock()
	found := false
	for _, resource := range data.ResourceMetrics {
		for _, scope := range resource.ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name == semconv.MetricAPIRequests {
					points := m.GetSum().DataPoints
					if len(points) != 1 || points[0].GetAsDouble() != 3 {
						t.Fatalf("OTLP counter duplicated: %v", points)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("OTLP metric lost or renamed")
	}
	batch := p.Emitter.(SnapshotBatchEmitter)
	for _, state := range []struct {
		value float64
		ttl   time.Duration
		empty bool
	}{{1, time.Minute, false}, {2, time.Minute, false}, {0, time.Minute, true}, {3, time.Nanosecond, false}} {
		points := []GaugePoint{{Value: state.value}}
		if state.empty {
			points = nil
		}
		if err := batch.GaugeSnapshots(ctx, state.ttl, map[string][]GaugePoint{semconv.MetricCertificatePack: points, semconv.MetricCertificateExpiry: points}); err != nil {
			t.Fatal(err)
		}
		text := scrape()
		parser := expfmt.NewTextParser(model.LegacyValidation)
		families, err := parser.TextToMetricFamilies(strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		present := !state.empty && state.ttl > time.Nanosecond
		for _, name := range []string{"cloudflare_certificate_pack", "cloudflare_certificate_expiry_seconds"} {
			family := families[name]
			if present {
				if family == nil || len(family.Metric) != 1 || family.Metric[0].GetGauge().GetValue() != state.value {
					t.Fatalf("incoherent Prometheus snapshot %s: %v", name, family)
				}
			} else if family != nil && len(family.Metric) > 0 {
				t.Fatalf("cleared/expired snapshot retained %s", name)
			}
		}
		if err := p.metrics.ForceFlush(ctx); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		latest := exports[len(exports)-1]
		mu.Unlock()
		values := map[string]float64{}
		for _, resource := range latest.ResourceMetrics {
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == semconv.MetricCertificatePack || metric.Name == semconv.MetricCertificateExpiry {
						if gauge := metric.GetGauge(); gauge != nil {
							for _, point := range gauge.DataPoints {
								values[metric.Name] = point.GetAsDouble()
							}
						}
					}
				}
			}
		}
		if present {
			if len(values) != 2 || values[semconv.MetricCertificatePack] != state.value || values[semconv.MetricCertificateExpiry] != state.value {
				t.Fatalf("incoherent OTLP snapshot: %v", values)
			}
		} else if len(values) != 0 {
			t.Fatalf("OTLP retained expired/empty snapshot: %v", values)
		}
	}
	after, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("private registry modified global registry")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/health", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected route: %d", res.StatusCode)
	}
}
