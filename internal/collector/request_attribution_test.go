package collector_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// This fixture uses the application's observer wiring, not a second source of
// request metrics. The outcome assertions also reject the baseline wiring.
func attributionAPI(cfg config.CloudflareConfig, emitter telemetry.Emitter) *cfapi.HTTPClient {
	return cfapi.NewRequestObserved(cfg, telemetry.APIRequestObserver(emitter))
}

type attributedREST struct{ api *cfapi.HTTPClient }

func (*attributedREST) Name() string                   { return "fixture.rest" }
func (*attributedREST) DefaultInterval() time.Duration { return time.Minute }
func (c *attributedREST) Collect(ctx context.Context, _ telemetry.Emitter) error {
	var rows []any
	if err := c.api.Get(ctx, "/redirect", nil, &rows); err != nil {
		return err
	}
	return c.api.Pages(ctx, "/pages", nil, 1, &rows)
}

type attributedGraph struct{ api *cfapi.HTTPClient }

func (*attributedGraph) Name() string                   { return "fixture.graph" }
func (*attributedGraph) DefaultInterval() time.Duration { return time.Minute }
func (*attributedGraph) Lag() time.Duration             { return 0 }
func (c *attributedGraph) CollectWindow(ctx context.Context, _, to time.Time, _ telemetry.Emitter) (time.Time, error) {
	var rows []any
	err := c.api.Query(ctx, cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: "fixture-zone", Dataset: "fixtureGroups", WantedFields: []string{"count"}, From: to.Add(-time.Minute), To: to}, &rows)
	return to, err
}

// A fresh subprocess owns the immutable process limiter. No production default
// or sibling test's shared budget is reset to arrange the slow-wait witness.
func TestRegisteredCollectorsRequestAttribution(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_ATTRIBUTION_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRegisteredCollectorsRequestAttribution$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_ATTRIBUTION_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("request attribution process: %v\n%s", err, output)
		}
		return
	}
	if err := cfapi.ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 40, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	var attempts, retries, settings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/retry", http.StatusFound)
		case "/retry":
			if retries.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		case "/pages":
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`{"result":[{}]}`))
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		case "/graphql":
			var query map[string]string
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
				t.Error(err)
			}
			if strings.Contains(query["query"], "settings") {
				settings.Add(1)
				_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"fixtureGroups":{"enabled":true,"availableFields":["count"],"maxNumberOfFields":1,"maxDuration":3600,"maxPageSize":100}}}]}}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"fixtureGroups":[]}]}}}`))
			}
		case "/outside":
			_, _ = w.Write([]byte(`{"result":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	emitter := telemetry.NewEmitter(provider.Meter("request-attribution"), lognoop.NewLoggerProvider().Logger("fixture"), tracenoop.NewTracerProvider().Tracer("fixture"))
	api := attributionAPI(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second}, emitter)
	registry := collector.NewRegistry()
	registry.RegisterSnapshot(&attributedREST{api: api}, time.Minute)
	graph := &attributedGraph{api: api}
	registry.RegisterWindow(graph, time.Minute, time.Minute, time.Minute)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(registry, emitter, store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, entry := range registry.Entries() {
		wg.Go(func() {
			if err := scheduler.RunOnce(ctx, entry); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	to := time.Now().Truncate(time.Second)
	if err := scheduler.CollectRange(ctx, graph, to.Add(-time.Minute), to); err != nil {
		t.Fatal(err)
	}
	var outside []any
	if err := api.Get(ctx, "/outside", nil, &outside); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 10 || settings.Load() != 1 {
		t.Fatalf("physical requests=%d settings=%d; want 10 and 1 (cached settings)", attempts.Load(), settings.Load())
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	counts := map[string]float64{}
	histCounts := map[string]map[string]uint64{}
	waitSums := map[string]float64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != semconv.MetricAPIRequests && m.Name != semconv.MetricAPIDuration && m.Name != "cf2otel.api.limiter_wait" {
				continue
			}
			switch points := m.Data.(type) {
			case metricdata.Sum[float64]:
				if m.Name != semconv.MetricAPIRequests || !points.IsMonotonic {
					t.Fatalf("unexpected sum %s", m.Name)
				}
				for _, p := range points.DataPoints {
					counts[attributionKey(t, p.Attributes)] += p.Value
				}
			case metricdata.Histogram[float64]:
				if m.Unit != "s" {
					t.Errorf("%s unit=%s", m.Name, m.Unit)
				}
				histCounts[m.Name] = map[string]uint64{}
				for _, p := range points.DataPoints {
					key := attributionKey(t, p.Attributes)
					histCounts[m.Name][key] += p.Count
					if m.Name == "cf2otel.api.limiter_wait" {
						waitSums[key] += p.Sum
					}
				}
			default:
				t.Fatalf("unexpected aggregation for %s: %T", m.Name, m.Data)
			}
		}
	}
	want := map[string]float64{"fixture.rest/rest/": 5, "fixture.rest/rest/rate_limited": 1, "fixture.graph/graphql/": 3, "unattributed/rest/": 1}
	if len(counts) != len(want) {
		t.Errorf("request series=%v, want=%v", counts, want)
	}
	for key, count := range want {
		if counts[key] != count {
			t.Errorf("%s requests=%v want=%v", key, counts[key], count)
		}
		for _, name := range []string{semconv.MetricAPIDuration, "cf2otel.api.limiter_wait"} {
			if histCounts[name][key] != uint64(count) {
				t.Errorf("%s %s samples=%d want=%v", name, key, histCounts[name][key], count)
			}
		}
	}
	for _, key := range []string{"fixture.rest/rest/", "fixture.graph/graphql/"} {
		if waitSums[key] < 0.005 {
			t.Errorf("%s limiter wait=%v; slow process budget not observed", key, waitSums[key])
		}
	}
}

func attributionKey(t *testing.T, attrs attribute.Set) string {
	t.Helper()
	var collectorName, method, class string
	for _, a := range attrs.ToSlice() {
		switch string(a.Key) {
		case semconv.AttrCollector:
			collectorName = a.Value.AsString()
		case semconv.AttrAPIMethod:
			method = a.Value.AsString()
		case semconv.AttrErrorClass:
			class = a.Value.AsString()
			if class == "" {
				t.Error("empty error class must be absent on success")
			}
		default:
			t.Errorf("forbidden request dimension %s", a.Key)
		}
	}
	if collectorName == "" || (method != "rest" && method != "graphql") {
		t.Errorf("missing collector or bounded method: %v", attrs)
	}
	return collectorName + "/" + method + "/" + class
}
