package loadbalancers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/loadbalancers"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestMain(m *testing.M) {
	// Real loopback fixtures retain caller deadlines, using explicit process pacing.
	if err := cfapi.ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 1}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// Literal names keep the base witness compilable with only an inert domain seam.
func setup(t *testing.T, h http.HandlerFunc, interval time.Duration, cap int) (collector.SnapshotCollector, telemetry.Emitter, *sdkmetric.ManualReader) {
	t.Helper()
	return setupAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			respond(w, []any{})
			return
		}
		h(w, r)
	}, interval, cap)
}

func setupAPI(t *testing.T, h http.HandlerFunc, interval time.Duration, cap int) (collector.SnapshotCollector, telemetry.Emitter, *sdkmetric.ManualReader) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "opaque-account"
	cfg.Cloudflare.APIBase = s.URL
	cfg.Collectors["loadbalancers.health"] = config.CollectorConfig{Enabled: true, Interval: interval}
	cfg.Platform.MaxMetricSeriesPerWindow = cap
	r := collector.NewRegistry()
	loadbalancers.Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: r})
	if len(r.Entries()) != 1 {
		t.Fatalf("enabled health must register one snapshot collector; got %d", len(r.Entries()))
	}
	c, ok := r.Entries()[0].Collector.(collector.SnapshotCollector)
	if !ok {
		t.Fatal("health must be a snapshot collector")
	}
	reader := sdkmetric.NewManualReader()
	p := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	e := telemetry.NewEmitter(p.Meter("fixture"), lognoop.NewLoggerProvider().Logger("fixture"), tracenoop.NewTracerProvider().Tracer("fixture"))
	return c, e, reader
}
func respond(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}
func row(id, name string) map[string]string { return map[string]string{"id": id, "name": name} }
func run(c collector.SnapshotCollector, e telemetry.Emitter) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Collect(ctx, e)
}
func points(t *testing.T, r *sdkmetric.ManualReader) map[string]float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "cloudflare.loadbalancers.pool.health" || m.Unit != "1" {
				t.Fatalf("unexpected signal/unit %s %s", m.Name, m.Unit)
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatal("not a gauge")
			}
			for _, p := range g.DataPoints {
				if p.Attributes.Len() != 1 {
					t.Fatal("ID/address/RTT or unexpected attributes")
				}
				name, ok := p.Attributes.Value(attribute.Key("cloudflare.loadbalancers.pool.name"))
				if !ok {
					t.Fatal("missing pool name")
				}
				out[name.AsString()] = p.Value
			}
		}
	}
	return out
}
func TestRegisteredHealthDirectFlags(t *testing.T) {
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.RawQuery != "" {
			t.Error("only documented query-free GET paths allowed")
		}
		if strings.HasSuffix(r.URL.Path, "/pools") {
			respond(w, []any{row("opaque-a", "alpha"), row("opaque-b", "beta"), row("opaque-c", "gamma")})
			return
		}
		switch r.URL.Path {
		case "/accounts/opaque-account/load_balancers/pools/opaque-a/health":
			respond(w, map[string]any{"pop_health": map[string]any{"healthy": true, "origins": []any{map[string]any{"ip": map[string]any{"rtt": "uninterpreted", "healthy": false}}}}})
		case "/accounts/opaque-account/load_balancers/pools/opaque-b/health":
			respond(w, map[string]any{"pop_health": map[string]any{"healthy": false}})
		case "/accounts/opaque-account/load_balancers/pools/opaque-c/health":
			respond(w, map[string]any{"pop_health": map[string]any{"healthy": nil}})
		default:
			t.Error("unexpected path")
			http.Error(w, "fixture failure", 400)
		}
	}, time.Minute, 500)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	got := points(t, r)
	if len(got) != 2 || got["alpha"] != 1 || got["beta"] != 0 {
		t.Fatalf("direct true/false flags with unknown omitted: %v", got)
	}
}

func TestRegisteredHealthEmptyUnknownFailureAndExpiry(t *testing.T) {
	phase := "known"
	detailCalls := 0
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pools") {
			if phase == "empty" {
				respond(w, []any{})
				return
			}
			if phase == "catalog-fail" {
				http.Error(w, "fixture failure", 400)
				return
			}
			if phase == "incomplete" {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{row("opaque-a", "alpha")}, "result_info": map[string]any{"total_count": 2}})
				return
			}
			respond(w, []any{row("opaque-a", "alpha"), row("opaque-b", "beta")})
			return
		}
		detailCalls++
		if phase == "failed" || (phase == "subset" && strings.Contains(r.URL.Path, "opaque-b")) {
			http.Error(w, "fixture failure", 400)
			return
		}
		flag := any(true)
		if phase == "unknown" {
			flag = nil
		}
		respond(w, map[string]any{"pop_health": map[string]any{"healthy": flag}})
	}, 30*time.Millisecond, 500)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	if len(points(t, r)) != 2 {
		t.Fatal("missing known snapshot")
	}
	phase = "subset"
	if err := run(c, e); err == nil {
		t.Fatal("subset failure must be reported")
	}
	if got := points(t, r); len(got) != 1 || got["alpha"] != 1 {
		t.Fatal("valid subset must replace failed pool", got)
	}
	phase = "failed"
	if err := run(c, e); err == nil {
		t.Fatal("all failed must report error")
	}
	if len(points(t, r)) != 1 {
		t.Fatal("all failed must retain prior unexpired snapshot")
	}
	time.Sleep(110 * time.Millisecond)
	if len(points(t, r)) != 0 {
		t.Fatal("failed poll refreshed expiry")
	}
	phase = "known"
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	phase = "unknown"
	if err := run(c, e); err == nil {
		t.Fatal("all unknown must not count as successful healthy classification")
	}
	if len(points(t, r)) != 0 {
		t.Fatal("all unknown must clear old known flags, not fabricate zero")
	}
	phase = "known"
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	phase = "catalog-fail"
	if err := run(c, e); err == nil {
		t.Fatal("failed catalog must report error")
	}
	if len(points(t, r)) != 2 {
		t.Fatal("failed catalog cleared prior snapshot")
	}
	phase = "incomplete"
	before := detailCalls
	if err := run(c, e); err == nil {
		t.Fatal("disclosed partial catalog must fail, not silently truncate")
	}
	if detailCalls != before || len(points(t, r)) != 2 {
		t.Fatal("incomplete catalog touched details or replaced snapshot")
	}
	phase = "empty"
	before = detailCalls
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	if detailCalls != before || len(points(t, r)) != 0 {
		t.Fatal("genuine empty catalog must clear with no health read")
	}
}

func TestRegisteredHealthCatalogValidationAndStickyRemainder(t *testing.T) {
	rows := []any{row("opaque-a", "alpha"), row("opaque-b", "beta"), row("opaque-c", "gamma"), row("opaque-d", "delta")}
	flags := map[string]any{"opaque-a": true, "opaque-b": true, "opaque-c": false, "opaque-d": nil}
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pools") {
			respond(w, rows)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		respond(w, map[string]any{"pop_health": map[string]any{"healthy": flags[parts[len(parts)-2]]}})
	}, time.Minute, 2)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	got := points(t, r)
	if len(got) != 2 || got["alpha"] != 1 || got["other"] != 0 {
		t.Fatal("cap must reserve one other slot; minimum excludes unknown flags", got)
	}
	rows = []any{row("opaque-b", "beta")}
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	got = points(t, r)
	if len(got) != 1 || got["other"] != 1 {
		t.Fatal("named admission must remain sticky across polls", got)
	}
	for _, invalid := range [][]any{
		{row("opaque-a", "duplicate"), row("opaque-b", "duplicate")},
		{row("opaque-a", "alpha"), row("opaque-a", "beta")},
		{row("opaque-a", "")},
		{row("opaque-a", strings.Repeat("x", 129))},
		{row("opaque-a", "bad\nname")},
	} {
		rows = invalid
		if err := run(c, e); err == nil {
			t.Fatal("invalid or ambiguous catalog accepted")
		}
		if len(points(t, r)) != 1 {
			t.Fatal("invalid catalog replaced valid snapshot")
		}
	}
	rows = nil
	for i := 0; i < 1000; i++ {
		rows = append(rows, row(fmt.Sprintf("opaque-%d", i), fmt.Sprintf("pool-%d", i)))
	}
	if err := run(c, e); err == nil {
		t.Fatal("catalog bound exhaustion must fail, never truncate")
	}
}

func TestHealthDisabledByDefault(t *testing.T) {
	cfg := config.Default()
	r := collector.NewRegistry()
	loadbalancers.Register(collector.Deps{Config: &cfg, Registry: r})
	if len(r.Entries()) != 0 {
		t.Fatal("default must not register or read pools")
	}
	if cfg.Collector("loadbalancers.health").Interval != 5*time.Minute {
		t.Fatal("default interval must be five minutes")
	}
}
