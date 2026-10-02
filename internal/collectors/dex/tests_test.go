package dex_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/dex"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// String names allow this test to run against the initial inert seam, without
// declaring production signal names outside semconv.
func setup(t *testing.T, h http.HandlerFunc, interval time.Duration, configure func(*config.Config)) (collector.SnapshotCollector, telemetry.Emitter, *sdkmetric.ManualReader) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "opaque-account"
	cfg.Cloudflare.APIBase = server.URL
	cfg.Cloudflare.APIToken = "fixture-token"
	cfg.Collectors["dex.tests"] = config.CollectorConfig{Enabled: true, Interval: interval}
	if configure != nil {
		configure(&cfg)
	}
	registry := collector.NewRegistry()
	dex.Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	if len(registry.Entries()) != 1 {
		t.Fatalf("enabled DEX must register one snapshot collector; got %d", len(registry.Entries()))
	}
	c, ok := registry.Entries()[0].Collector.(collector.SnapshotCollector)
	if !ok {
		t.Fatal("DEX must be a snapshot collector")
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	e := telemetry.NewEmitter(provider.Meter("fixture"), lognoop.NewLoggerProvider().Logger("fixture"), tracenoop.NewTracerProvider().Tracer("fixture"))
	return c, e, reader
}
func respond(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}
func catalog(rows ...map[string]string) any {
	if rows == nil {
		rows = []map[string]string{}
	}
	return map[string]any{"tests": rows}
}
func row(id, name, kind string) map[string]string {
	return map[string]string{"id": id, "name": name, "kind": kind}
}
func avg(v any) any { return map[string]any{"avg": v, "slots": []any{map[string]any{"value": 999}}} }
func httpResult(v any) any {
	return map[string]any{"httpStats": map[string]any{"resourceFetchTimeMs": avg(v), "availabilityPct": avg(nil)}}
}
func traceResult() any {
	return map[string]any{"tracerouteStats": map[string]any{"roundTripTimeMs": avg(40), "hopsCount": avg(4.5), "packetLossPct": avg(12.5), "availabilityPct": avg(87.5)}}
}
func collect(t *testing.T, c collector.SnapshotCollector, e telemetry.Emitter) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.Collect(ctx, e)
}
func measurements(t *testing.T, r *sdkmetric.ManualReader) map[string][]metricdata.DataPoint[float64] {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	result := map[string][]metricdata.DataPoint[float64]{}
	for _, s := range data.ScopeMetrics {
		for _, m := range s.Metrics {
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("%s is not gauge", m.Name)
			}
			unit := "%"
			if strings.HasSuffix(m.Name, "fetch_time") || strings.HasSuffix(m.Name, "rtt") {
				unit = "ms"
			}
			if strings.HasSuffix(m.Name, "hops") {
				unit = "{hop}"
			}
			if m.Unit != unit {
				t.Fatalf("%s unit %s want %s", m.Name, m.Unit, unit)
			}
			for _, p := range g.DataPoints {
				if p.Attributes.Len() != 2 {
					t.Fatalf("unexpected attributes: %v", p.Attributes)
				}
				for _, a := range p.Attributes.ToSlice() {
					if string(a.Key) != "cloudflare.dex.test.name" && string(a.Key) != "cloudflare.dex.test.kind" {
						t.Fatalf("identifier/unexpected attribute: %s", a.Key)
					}
				}
			}
			result[m.Name] = g.DataPoints
		}
	}
	return result
}
func label(p metricdata.DataPoint[float64], key string) string {
	v, _ := p.Attributes.Value(attribute.Key(key))
	return v.AsString()
}
func TestDEXDisabledByDefault(t *testing.T) {
	cfg := config.Default()
	registry := collector.NewRegistry()
	dex.Register(collector.Deps{Config: &cfg, Registry: registry})
	if len(registry.Entries()) != 0 {
		t.Fatal("default DEX must not register or read upstream")
	}
}

func TestRegisteredDEXRemainderMeansByKind(t *testing.T) {
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/overview") {
			respond(w, catalog(row("opaque-a", "probe-alpha", "http"), row("opaque-b", "probe-beta", "http"), row("opaque-c", "probe-gamma", "traceroute"), row("opaque-d", "probe-delta", "traceroute")))
			return
		}
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "opaque-a"):
			result = map[string]any{"httpStats": map[string]any{"resourceFetchTimeMs": avg(20), "availabilityPct": avg(90)}}
		case strings.HasSuffix(r.URL.Path, "opaque-b"):
			result = map[string]any{"httpStats": map[string]any{"resourceFetchTimeMs": avg(80), "availabilityPct": avg(50)}}
		case strings.HasSuffix(r.URL.Path, "opaque-c"):
			result = map[string]any{"tracerouteStats": map[string]any{"roundTripTimeMs": avg(40), "hopsCount": avg(2), "packetLossPct": avg(10), "availabilityPct": avg(60)}}
		case strings.HasSuffix(r.URL.Path, "opaque-d"):
			result = map[string]any{"tracerouteStats": map[string]any{"roundTripTimeMs": avg(100), "hopsCount": avg(6), "packetLossPct": avg(30), "availabilityPct": avg(100)}}
		default:
			t.Error("unexpected detail path")
		}
		respond(w, result)
	}, time.Minute, func(c *config.Config) { c.DEX.MaxMetricSeries = 6 })
	if err := collect(t, c, e); err != nil {
		t.Fatal(err)
	}
	points := measurements(t, r)
	seen := 0
	for name, values := range points {
		for _, p := range values {
			seen++
			kind := label(p, "cloudflare.dex.test.kind")
			if label(p, "cloudflare.dex.test.name") != "other" {
				t.Fatal("minimum cap must reserve only remainders")
			}
			want := map[string]float64{"cloudflare.dex.http.fetch_time": 50, "cloudflare.dex.traceroute.rtt": 70, "cloudflare.dex.traceroute.hops": 4, "cloudflare.dex.packet_loss": 20, "cloudflare.dex.availability": 80}[name]
			if name == "cloudflare.dex.availability" && kind == "http" {
				want = 70
			}
			if p.Value != want {
				t.Fatalf("%s/%s per-test average: got %v want %v", name, kind, p.Value, want)
			}
		}
	}
	if seen != 6 {
		t.Fatalf("want six distinct kind/signal remainder points, got %d", seen)
	}
}

func TestRegisteredDEXResultsAndUnits(t *testing.T) {
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("not read-only")
		}
		if strings.HasSuffix(r.URL.Path, "/overview") {
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "50" {
				t.Error("list paging")
			}
			respond(w, catalog(row("opaque-http", "probe-alpha", "http"), row("opaque-trace", "probe-beta", "traceroute")))
			return
		}
		q := r.URL.Query()
		from, err := time.Parse("2006-01-02T15:04:05.000Z", q.Get("from"))
		if err != nil {
			t.Error(err)
		}
		to, err := time.Parse("2006-01-02T15:04:05.000Z", q.Get("to"))
		if err != nil {
			t.Error(err)
		}
		if to.Sub(from) != time.Hour || q.Get("interval") != "minute" || len(q) != 3 {
			t.Error("detail window/query contract")
		}
		if strings.HasSuffix(r.URL.Path, "/http-tests/opaque-http") {
			respond(w, httpResult(125))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/traceroute-tests/opaque-trace") {
			respond(w, traceResult())
			return
		}
		t.Error("unexpected endpoint")
		w.WriteHeader(400)
	}, time.Minute, nil)
	if err := collect(t, c, e); err != nil {
		t.Fatal(err)
	}
	points := measurements(t, r)
	for name, want := range map[string]float64{"cloudflare.dex.http.fetch_time": 125, "cloudflare.dex.traceroute.rtt": 40, "cloudflare.dex.traceroute.hops": 4.5, "cloudflare.dex.packet_loss": 12.5, "cloudflare.dex.availability": 87.5} {
		p := points[name]
		if len(p) != 1 || p[0].Value != want {
			t.Fatalf("%s: %v want %v", name, p, want)
		}
	}
}
func TestRegisteredDEXRetryIsolationAndLifecycle(t *testing.T) {
	var mu sync.Mutex
	phase := 0
	calls := map[string]int{}
	setPhase := func(next int) { mu.Lock(); defer mu.Unlock(); phase = next }
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/overview") {
			if phase == 2 {
				respond(w, catalog())
				return
			}
			respond(w, catalog(row("opaque-a", "probe-alpha", "http"), row("opaque-b", "probe-beta", "traceroute"), row("opaque-c", "probe-gamma", "http")))
			return
		}
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		calls[id]++
		if phase == 1 || id == "opaque-c" {
			w.WriteHeader(400)
			return
		}
		if calls[id] == 1 {
			if id == "opaque-a" {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(504)
			}
			return
		}
		if id == "opaque-a" {
			respond(w, httpResult(125))
		} else {
			respond(w, traceResult())
		}
	}, 25*time.Millisecond, nil)
	if err := collect(t, c, e); err == nil {
		t.Fatal("one failed test must report safe collection error")
	}
	mu.Lock()
	httpCalls, traceCalls := calls["opaque-a"], calls["opaque-b"]
	mu.Unlock()
	if httpCalls != 2 || traceCalls != 2 {
		t.Fatalf("shared retries not observed: http=%d traceroute=%d", httpCalls, traceCalls)
	}
	if len(measurements(t, r)["cloudflare.dex.http.fetch_time"]) != 1 {
		t.Fatal("successful subset not published")
	}
	setPhase(1)
	if err := collect(t, c, e); err == nil {
		t.Fatal("all failed must report error")
	}
	if len(measurements(t, r)["cloudflare.dex.http.fetch_time"]) != 1 {
		t.Fatal("all failed cleared prior snapshot")
	}
	time.Sleep(90 * time.Millisecond)
	if len(measurements(t, r)["cloudflare.dex.http.fetch_time"]) != 0 {
		t.Fatal("all failed refreshed TTL")
	}
	setPhase(0)
	if err := collect(t, c, e); err == nil {
		t.Fatal("failed test hidden")
	}
	setPhase(2)
	if err := collect(t, c, e); err != nil {
		t.Fatal(err)
	}
	if len(measurements(t, r)["cloudflare.dex.http.fetch_time"]) != 0 {
		t.Fatal("empty catalog did not clear")
	}
}
func TestRegisteredDEXPaginationAndNameCap(t *testing.T) {
	var mu sync.Mutex
	mode := "complete"
	pages := 0
	details := 0
	setMode := func(next string) { mu.Lock(); defer mu.Unlock(); mode = next; details = 0 }
	counts := func() (int, int) { mu.Lock(); defer mu.Unlock(); return pages, details }
	c, e, r := setup(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/overview") {
			pages++
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			rows := []map[string]string{}
			if mode == "sticky" {
				rows = append(rows, row("opaque-new", "aaa", "http"), row("opaque-0", "probe-00", "http"), row("opaque-later", "probe-later", "http"))
			} else if mode == "ambiguous" {
				rows = append(rows, row("opaque-a", "same-name", "http"), row("opaque-b", "same-name", "http"))
			} else if page == 1 || mode == "repeated" {
				for i := 0; i < 50; i++ {
					rows = append(rows, row(fmt.Sprintf("opaque-%d", i), fmt.Sprintf("probe-%02d", i), "http"))
				}
			} else if page == 2 {
				rows = append(rows, row("opaque-last", "probe-last", "http"))
			}
			respond(w, catalog(rows...))
			return
		}
		details++
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		value := 500
		if id == "opaque-new" {
			value = 30
		} else if id == "opaque-later" {
			value = 90
		} else if id != "opaque-last" {
			i, err := strconv.Atoi(strings.TrimPrefix(id, "opaque-"))
			if err != nil {
				t.Error(err)
			}
			value = 10 * i
			if i == 0 {
				value = 100
			}
		}
		respond(w, httpResult(value))
	}, time.Minute, func(c *config.Config) { c.DEX.MaxMetricSeries = 7; c.DEX.MaxTests = 51 })
	if err := collect(t, c, e); err != nil {
		t.Fatal(err)
	}
	pageCount, detailCount := counts()
	if pageCount != 2 || detailCount != 51 {
		t.Fatalf("incomplete enumeration: pages=%d details=%d", pageCount, detailCount)
	}
	p := measurements(t, r)["cloudflare.dex.http.fetch_time"]
	if len(p) != 2 {
		t.Fatalf("cap must reserve six per-kind signal remainder slots: %v", p)
	}
	found := false
	for _, p := range p {
		if label(p, "cloudflare.dex.test.name") == "other" {
			found = true
			if p.Value != 255 {
				t.Fatalf("remainder must average fifty different per-test provider averages: got %v want 255", p.Value)
			}
		}
	}
	if !found {
		t.Fatal("missing remainder")
	}
	setMode("sticky")
	if err := collect(t, c, e); err != nil {
		t.Fatal(err)
	}
	p = measurements(t, r)["cloudflare.dex.http.fetch_time"]
	if len(p) != 2 {
		t.Fatalf("sticky cap changed size: %v", p)
	}
	for _, point := range p {
		switch label(point, "cloudflare.dex.test.name") {
		case "probe-00":
			if point.Value != 100 {
				t.Fatal("admitted test changed value")
			}
		case "other":
			if point.Value != 60 {
				t.Fatalf("new lexical-first test must remain in remainder: %v", point.Value)
			}
		default:
			t.Fatal("sticky admission displaced prior test")
		}
	}
	for _, m := range []string{"ambiguous", "repeated"} {
		setMode(m)
		if err := collect(t, c, e); err == nil {
			t.Fatalf("%s must fail", m)
		}
		_, detailCount := counts()
		if detailCount != 0 {
			t.Fatal("invalid catalog published/fetched results")
		}
	}
}
