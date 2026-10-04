package warp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/warp"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
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

func row(id, status string) string {
	b, _ := json.Marshal(map[string]string{"deviceId": id, "timestamp": "opaque-time", "status": status, "platform": "fixture-platform", "version": "fixture-version", "mode": "fixture-mode", "colo": "fixture-colo"})
	return string(b)
}
func array(rows ...string) string { return "[" + strings.Join(rows, ",") + "]" }
func setup(t *testing.T, handler http.HandlerFunc, interval time.Duration, limit int) (collector.SnapshotCollector, telemetry.Emitter, *sdkmetric.ManualReader) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "fixture/account"
	cfg.Cloudflare.APIBase = server.URL
	cfg.Cloudflare.APIToken = "fixture-token"
	cfg.WARP.LastSeenWindow = 12 * time.Minute
	cfg.WARP.MaxMetricSeries = limit
	cfg.Collectors[semconv.CollectorNameWARPFleet] = config.CollectorConfig{Enabled: true, Interval: interval}
	registry := collector.NewRegistry()
	warp.Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 || entries[0].Interval != interval {
		t.Fatalf("registry: %v", entries)
	}
	c, ok := entries[0].Collector.(collector.SnapshotCollector)
	if !ok {
		t.Fatal("not a snapshot collector")
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	emitter := telemetry.NewEmitter(provider.Meter("fixture"), lognoop.NewLoggerProvider().Logger("fixture"), tracenoop.NewTracerProvider().Tracer("fixture"))
	return c, emitter, reader
}
func points(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.DataPoint[float64] {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	var points []metricdata.DataPoint[float64]
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != semconv.MetricWARPDevices {
				t.Fatalf("unexpected metric %s", metric.Name)
			}
			if metric.Unit != "1" {
				t.Fatalf("unit %q", metric.Unit)
			}
			gauge, ok := metric.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatal("not a gauge")
			}
			points = append(points, gauge.DataPoints...)
		}
	}
	allowed := map[string]bool{semconv.AttrWARPStatus: true, semconv.AttrWARPPlatform: true, semconv.AttrWARPClientVersion: true, semconv.AttrWARPMode: true, semconv.AttrWARPColo: true, semconv.AttrWARPRemainder: true}
	for _, p := range points {
		if p.Attributes.Len() != 6 {
			t.Fatalf("unexpected attributes %v", p.Attributes)
		}
		for _, a := range p.Attributes.ToSlice() {
			if !allowed[string(a.Key)] {
				t.Fatalf("identity/unexpected attribute %s", a.Key)
			}
		}
	}
	return points
}
func value(p metricdata.DataPoint[float64], key string) string {
	v, _ := p.Attributes.Value(attribute.Key(key))
	return v.AsString()
}
func total(points []metricdata.DataPoint[float64]) float64 {
	var n float64
	for _, p := range points {
		n += p.Value
	}
	return n
}

func TestRegisteredPaginationAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages []string
		size  int
		want  float64
		fail  bool
	}{
		{"capped-full-page-needs-empty", []string{array(row("opaque-a", "observed-a"), row("opaque-b", "observed-a")), array(row("opaque-c", "observed-b"), row("opaque-c", "observed-b")), "[]"}, 2, 3, false},
		{"duplicate-identical-short-page", []string{array(row("opaque-a", "observed-a"), row("opaque-b", "observed-b")), array(row("opaque-a", "observed-a"))}, 2, 2, false},
		{"duplicate-conflict", []string{array(row("opaque-a", "observed-a"), row("opaque-b", "observed-b")), array(row("opaque-a", "observed-b"))}, 2, 0, true},
		{"stuck-full-page", []string{array(row("opaque-a", "observed-a")), array(row("opaque-a", "observed-a"))}, 1, 0, true},
		{"null-result", []string{"null"}, 50, 0, true},
		{"missing-result", []string{""}, 50, 0, true},
		{"wrong-result", []string{`{}`}, 50, 0, true},
		{"null-required-string", []string{array(strings.Replace(row("opaque-a", "observed-a"), `"platform":"fixture-platform"`, `"platform":null`, 1))}, 50, 0, true},
		{"wrong-required-type", []string{array(strings.Replace(row("opaque-a", "observed-a"), `"version":"fixture-version"`, `"version":false`, 1))}, 50, 0, true},
		{"missing-required-string", []string{array(strings.Replace(row("opaque-a", "observed-a"), `"timestamp":"opaque-time",`, "", 1))}, 50, 0, true},
		{"empty-device-id", []string{array(row("", "observed-a"))}, 50, 0, true},
		{"metadata-mismatch", []string{array(row("opaque-a", "observed-a"))}, -1, 0, true},
		{"page-size-invalid", []string{"[]"}, 51, 0, true},
		{"HTTP-error", []string{"http-error"}, 50, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, e, reader := setup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				q := r.URL.Query()
				if r.Method != "GET" || r.URL.EscapedPath() != "/accounts/fixture%2Faccount/dex/fleet-status/devices" || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Errorf("wrong read-only request shape")
				}
				if q.Get("source") != "last_seen" || q.Get("per_page") != "50" || len(q) != 5 {
					t.Errorf("unexpected query %v", q)
				}
				from, err1 := time.Parse("2006-01-02T15:04:05.000Z", q.Get("from"))
				to, err2 := time.Parse("2006-01-02T15:04:05.000Z", q.Get("to"))
				if err1 != nil || err2 != nil || to.Sub(from) != 12*time.Minute {
					t.Errorf("unbounded/wrong query window")
				}
				page, _ := strconv.Atoi(q.Get("page"))
				if page != calls || page > len(tc.pages) {
					t.Errorf("unexpected page %d", page)
					http.Error(w, "fixture", 400)
					return
				}
				body := tc.pages[page-1]
				if body == "http-error" {
					http.Error(w, "fixture", 400)
					return
				}
				if body == "" {
					fmt.Fprint(w, `{"success":true}`)
					return
				}
				reportedPage := page
				size := tc.size
				if size == -1 {
					reportedPage++
					size = 50
				}
				fmt.Fprintf(w, `{"success":true,"result":%s,"result_info":{"page":%d,"per_page":%d,"total_count":0,"total_pages":1}}`, body, reportedPage, size)
			}, time.Minute, 500)
			err := c.Collect(context.Background(), e)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v want failure=%v", err, tc.fail)
			}
			if err != nil && (strings.Contains(err.Error(), "opaque-a") || strings.Contains(err.Error(), "fixture-platform")) {
				t.Fatal("device value leaked")
			}
			got := points(t, reader)
			if total(got) != tc.want {
				t.Fatalf("count=%v want %v", total(got), tc.want)
			}
			if !tc.fail && calls != len(tc.pages) {
				t.Fatalf("incomplete pagination %d", calls)
			}
			if tc.name == "capped-full-page-needs-empty" {
				byStatus := map[string]float64{}
				for _, p := range got {
					byStatus[value(p, semconv.AttrWARPStatus)] = p.Value
				}
				if byStatus["observed-a"] != 2 || byStatus["observed-b"] != 1 {
					t.Fatalf("raw status counts %v", byStatus)
				}
			}
		})
	}
}

func TestSnapshotReplacementFailureExpiryAndEmpty(t *testing.T) {
	body := array(row("opaque-a", "observed-a"))
	c, e, reader := setup(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"success":true,"result":%s}`, body) }, 30*time.Millisecond, 500)
	poll := func() {
		t.Helper()
		if err := c.Collect(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	if total(points(t, reader)) != 1 {
		t.Fatal("initial snapshot absent")
	}
	body = array(row("opaque-a", "observed-b"))
	poll()
	got := points(t, reader)
	if len(got) != 1 || value(got[0], semconv.AttrWARPStatus) != "observed-b" {
		t.Fatal("old status retained")
	}
	body = "null"
	if err := c.Collect(context.Background(), e); err == nil {
		t.Fatal("malformed poll succeeded")
	}
	if total(points(t, reader)) != 1 {
		t.Fatal("malformed poll cleared snapshot")
	}
	time.Sleep(120 * time.Millisecond)
	if len(points(t, reader)) != 0 {
		t.Fatal("failed poll refreshed original TTL")
	}
	body = array(row("opaque-a", "observed-a"))
	poll()
	body = "[]"
	poll()
	if len(points(t, reader)) != 0 {
		t.Fatal("genuine empty did not clear old snapshot")
	}
}

func TestBoundedRemainderConservesCount(t *testing.T) {
	// An actual all-other tuple must remain distinct from the overflow bucket.
	other := `{"deviceId":"opaque-other","timestamp":"opaque-time","status":"other","platform":"other","version":"other","mode":"other","colo":"other"}`
	long := row("opaque-long", strings.Repeat("x", 257))
	body := array(other, row("opaque-a", "z-a"), row("opaque-b", "z-b"), long)
	c, e, reader := setup(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"success":true,"result":%s}`, body) }, time.Minute, 1)
	if err := c.Collect(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got := points(t, reader)
	if len(got) != 2 || total(got) != 4 {
		t.Fatalf("bounded count not conserved: %v", got)
	}
	counts := map[string]float64{}
	for _, p := range got {
		counts[value(p, semconv.AttrWARPRemainder)] = p.Value
		if value(p, semconv.AttrWARPStatus) != "other" {
			t.Fatal("lexical first tuple/remainder differs")
		}
	}
	if counts["false"] != 1 || counts["true"] != 3 {
		t.Fatalf("collision/overflow %v", counts)
	}
}

func TestDisabledAndCanceledAndEmitterCapability(t *testing.T) {
	cfg := config.Default()
	registry := collector.NewRegistry()
	warp.Register(collector.Deps{Config: &cfg, Registry: registry})
	if len(registry.Entries()) != 0 {
		t.Fatal("default collector enabled")
	}
	calls := 0
	c, e, reader := setup(t, func(w http.ResponseWriter, _ *http.Request) { calls++; fmt.Fprint(w, `{"success":true,"result":[]}`) }, time.Minute, 500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Collect(ctx, e); err == nil {
		t.Fatal("canceled poll succeeded")
	}
	if calls != 0 || len(points(t, reader)) != 0 {
		t.Fatal("canceled poll published or fetched")
	}
	if err := c.Collect(context.Background(), struct{ telemetry.Emitter }{e}); err == nil {
		t.Fatal("ordinary Gauge fallback accepted")
	}
	if calls != 0 {
		t.Fatal("unsupported emitter fetched")
	}
}
