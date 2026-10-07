package selfobs_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	colMetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// The imported selfobs package initializes before this test package.
var testProcessInitialized = time.Now()

// Observe the actual OTLP HTTP export, not only an emitter call or SDK registration.
func TestProcessStartRealExport(t *testing.T) {
	var mu sync.Mutex
	metrics := map[string]*metricpb.Metric{}
	var collectedAfter uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var req colMetric.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			for _, resource := range req.ResourceMetrics {
				for _, scope := range resource.ScopeMetrics {
					for _, metric := range scope.Metrics {
						// An older periodic collection can still be in flight when
						// Collect returns. Its arrival is not proof of fresh data.
						fresh := true
						for _, point := range metric.GetGauge().GetDataPoints() {
							if point.TimeUnixNano < collectedAfter {
								fresh = false
							}
						}
						if fresh {
							metrics[metric.Name] = metric
						}
					}
				}
			}
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	providers, err := telemetry.NewProviders(ctx, telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := providers.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
	}()
	beforeNew := time.Now()
	stats := selfobs.New(providers.Emitter, "fixture", "fixture")
	collect := selfobs.NewCollector(stats)
	assertExport := func() float64 {
		t.Helper()
		if err := collect.Collect(ctx, providers.Emitter); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		collectedAfter = uint64(time.Now().UnixNano())
		clear(metrics)
		mu.Unlock()
		// FlushCommit flushes logs/traces only; wait for the real periodic
		// metric reader to collect after this Collect returned, not merely
		// for an older export to arrive after the map was cleared.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ready := metrics["cf2otel.process.start_time"] != nil && metrics[semconv.MetricBuildInfo] != nil
			mu.Unlock()
			if ready {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		// Use the wire name so this regression also compiles against the base
		// that has no declaration and fails specifically for the missing export.
		metric := metrics["cf2otel.process.start_time"]
		if metric == nil {
			t.Fatal("missing exported cf2otel.process.start_time metric")
		}
		if metric.Unit != "s" || metric.Description != "Unix time the cf2otel process started." {
			t.Fatalf("process start metadata: unit=%q description=%q", metric.Unit, metric.Description)
		}
		points := metric.GetGauge().GetDataPoints()
		if len(points) != 1 {
			t.Fatalf("process start gauge points = %d, want 1", len(points))
		}
		if len(points[0].Attributes) != 0 {
			t.Fatalf("unexpected process start attributes: %v", points[0].Attributes)
		}
		if metrics[semconv.MetricBuildInfo] == nil {
			t.Fatal("missing build.info beside process start")
		}
		value := points[0].GetAsDouble()
		if value < float64(testProcessInitialized.Add(-time.Minute).UnixNano())/1e9 || value > float64(beforeNew.UnixNano())/1e9 {
			t.Fatalf("process start %v is not near process initialization and at or before Stats construction %v", value, beforeNew)
		}
		return value
	}
	first := assertExport()
	// Cross a whole second so even integer-second capture at New or Collect
	// cannot masquerade as process-wide initialization.
	time.Sleep(1100 * time.Millisecond)
	collect.Now = func() time.Time { return time.Now().Add(time.Hour) }
	if got := assertExport(); got != first {
		t.Fatalf("process start changed across collections: %v -> %v", first, got)
	}
	collect = selfobs.NewCollector(selfobs.New(providers.Emitter, "fixture", "fixture"))
	if got := assertExport(); got != first {
		t.Fatalf("process start changed across Stats instances: %v -> %v", first, got)
	}
}
