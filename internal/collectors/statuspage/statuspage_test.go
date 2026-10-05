package statuspage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/statuspage"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tests use literal contract names so registration absence is a behavioural red
// witness against the base, not a compile error from undeclared new constants.
const componentMetric = "cloudflare.status.component.status"
const componentName = "cloudflare.status.component.name"
const componentType = "cloudflare.status.component.type"

func registered(t *testing.T, url string, interval time.Duration, cap int) (*collector.Registry, collector.Entry, collector.Entry) {
	t.Helper()
	cfg := config.Default()
	cfg.Cloudflare.APIToken = "opaque-token"
	// Exercise the loader rather than reaching into the collector's private types.
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("statuspage:\n  base_url: %q\n  component_cap: %d\ncollectors:\n  statuspage.components:\n    enabled: true\n    interval: %s\n  statuspage.incidents:\n    enabled: true\n", url, cap, interval)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg = *loaded
	cfg.Cloudflare.APIToken = "opaque-token"
	cfg.Collectors["statuspage.components"] = config.CollectorConfig{Enabled: true, Interval: interval}
	cfg.Collectors["statuspage.incidents"] = config.CollectorConfig{Enabled: true, Interval: time.Minute, InitialLookback: time.Minute, MaxWindow: time.Hour}
	r := collector.NewRegistry()
	statuspage.Register(collector.Deps{Config: &cfg, Registry: r})
	var components, incidents collector.Entry
	for _, e := range r.Entries() {
		switch e.Collector.Name() {
		case "statuspage.components":
			components = e
		case "statuspage.incidents":
			incidents = e
		}
	}
	if components.Collector == nil || incidents.Collector == nil {
		t.Fatal("both independently enabled statuspage collectors must register")
	}
	if _, ok := components.Collector.(collector.SnapshotCollector); !ok {
		t.Fatal("components must use snapshot mode")
	}
	if _, ok := incidents.Collector.(collector.WindowCollector); !ok {
		t.Fatal("incidents must use window mode")
	}
	return r, components, incidents
}

func server(t *testing.T, summary, incidents *string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "cf2otel (+https://github.com/rknightion/cf2otel)" {
			t.Errorf("public API request violates method, query, authorization or user-agent contract")
		}
		switch r.URL.Path {
		case "/api/v2/summary.json":
			fmt.Fprint(w, *summary)
		case "/api/v2/incidents.json":
			fmt.Fprint(w, *incidents)
		default:
			t.Errorf("unexpected API path")
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

type exportedLogs struct{ records []sdklog.Record }

func (e *exportedLogs) Export(_ context.Context, records []sdklog.Record) error {
	for _, r := range records {
		e.records = append(e.records, r.Clone())
	}
	return nil
}
func (*exportedLogs) Shutdown(context.Context) error   { return nil }
func (*exportedLogs) ForceFlush(context.Context) error { return nil }
func emitter(t *testing.T) (telemetry.Emitter, *sdkmetric.ManualReader, *exportedLogs) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	logs := &exportedLogs{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()); _ = lp.Shutdown(context.Background()) })
	return telemetry.NewEmitter(mp.Meter("fixture"), lp.Logger("fixture"), noop.NewTracerProvider().Tracer("fixture")), reader, logs
}
func points(t *testing.T, reader *sdkmetric.ManualReader) map[string]float64 {
	t.Helper()
	var out metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, scope := range out.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != componentMetric {
				continue
			}
			if m.Unit != "1" {
				t.Fatalf("unit=%q", m.Unit)
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatal("component status is not a gauge")
			}
			for _, p := range g.DataPoints {
				name, _ := p.Attributes.Value(attribute.Key(componentName))
				typ, _ := p.Attributes.Value(attribute.Key(componentType))
				if p.Attributes.Len() != 2 {
					t.Fatal("component metrics must not carry IDs or other labels")
				}
				got[name.AsString()+"/"+typ.AsString()] = p.Value
			}
		}
	}
	return got
}
func run(t *testing.T, s *collector.Scheduler, e collector.Entry) {
	t.Helper()
	if err := s.RunOnce(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func TestComponentsStatusRefreshAndExpiry(t *testing.T) {
	summary := `{"components":[{"name":"alpha","status":"operational","group":false},{"name":"bravo","status":"under_maintenance","group":false},{"name":"charlie","status":"degraded_performance","group":true},{"name":"delta","status":"partial_outage","group":false},{"name":"echo","status":"major_outage","group":false},{"name":"foxtrot","status":"future_status","group":false}]}`
	incidents := `{"incidents":null}` // unrelated incident failure must not block components.
	srv := server(t, &summary, &incidents)
	r, c, i := registered(t, srv.URL, 200*time.Millisecond, 500)
	e, reader, _ := emitter(t)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(r, e, store)
	run(t, s, c)
	want := map[string]float64{"alpha/component": 0, "bravo/component": 1, "charlie/group": 2, "delta/component": 3, "echo/component": 4, "foxtrot/component": 5}
	if got := points(t, reader); !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses=%v", got)
	}
	if _, ok := store.Get(c.Collector.Name()); ok {
		t.Fatal("components must not create checkpoints")
	}
	if err := s.RunOnce(context.Background(), i); err == nil {
		t.Fatal("invalid incidents must fail independently")
	}
	summary = `{"components":[{"name":"alpha","status":"partial_outage","group":false}]}`
	run(t, s, c)
	if got := points(t, reader); !reflect.DeepEqual(got, map[string]float64{"alpha/component": 3}) {
		t.Fatalf("retired series retained: %v", got)
	}
	time.Sleep(300 * time.Millisecond)
	summary = `{"components":[{"name":"alpha","status":"operational","group":false},{"name":"bad","status":3,"group":false}]}`
	if err := s.RunOnce(context.Background(), c); err == nil {
		t.Fatal("typed invalid second row must fail")
	}
	if got := points(t, reader); !reflect.DeepEqual(got, map[string]float64{"alpha/component": 3}) {
		t.Fatalf("failed read changed snapshot: %v", got)
	}
	time.Sleep(400 * time.Millisecond)
	if got := points(t, reader); len(got) != 0 {
		t.Fatalf("snapshot failed to expire: %v", got)
	}
	summary = `{"components":[{"name":"alpha","status":"major_outage","group":false}]}`
	run(t, s, c)
	summary = `{"components":[]}`
	run(t, s, c)
	if got := points(t, reader); len(got) != 0 {
		t.Fatalf("empty valid snapshot did not clear: %v", got)
	}
}
func TestComponentsCapWorstDuplicateAndInvalidSchemas(t *testing.T) {
	summary := `{"components":[{"name":"zulu","status":"major_outage","group":false},{"name":"alpha","status":"operational","group":false},{"name":"alpha","status":"partial_outage","group":false},{"name":"theta","status":"degraded_performance","group":false},{"name":"other","status":"under_maintenance","group":true}]}`
	incidents := `{"incidents":[]}`
	srv := server(t, &summary, &incidents)
	r, c, _ := registered(t, srv.URL, time.Minute, 2)
	e, reader, _ := emitter(t)
	s := collector.NewScheduler(r, e, nil)
	run(t, s, c)
	want := map[string]float64{"alpha/component": 3, "other/group": 1, "other/remainder": 4}
	if got := points(t, reader); !reflect.DeepEqual(got, want) {
		t.Fatalf("cap must preserve worst duplicate and overflow: %v", got)
	}
	for _, bad := range []string{`{}`, `{"components":null}`, `{"components":[{"name":"bad","status":"operational"}]}`, `{"components":[{"name":null,"status":"operational","group":false}]}`} {
		summary = bad
		if err := s.RunOnce(context.Background(), c); err == nil {
			t.Fatalf("invalid schema accepted: %s", bad)
		}
		if got := points(t, reader); !reflect.DeepEqual(got, want) {
			t.Fatalf("invalid schema published: %v", got)
		}
	}
	if err := c.Collector.(collector.SnapshotCollector).Collect(context.Background(), &telemetry.Buffer{}); err == nil {
		t.Fatal("non-snapshot emitter must be rejected, not silently lose expiry")
	}
}
func incidentJSON(at time.Time, body string) string {
	// Opaque fixtures contain no real vendor names, IDs, email or tenant hosts.
	payload := map[string]any{"incidents": []any{map[string]any{"id": "incident-alpha", "name": "opaque-incident", "status": "resolved", "impact": "minor", "incident_updates": []any{map[string]any{"id": "update-alpha", "status": "resolved", "body": body, "updated_at": at.Format(time.RFC3339Nano)}, map[string]any{"id": "update-alpha", "status": "resolved", "body": body, "updated_at": at.Format(time.RFC3339Nano)}, map[string]any{"id": "update-alpha", "status": "monitoring", "body": "revision", "updated_at": at.Add(time.Minute).Format(time.RFC3339Nano)}}}}}
	b, _ := json.Marshal(payload)
	return string(b)
}
func logValue(r sdklog.Record, key string) attribute.Value {
	v := attribute.Value{}
	r.WalkAttributes(func(a attribute.KeyValue) bool {
		if string(a.Key) == key {
			v = a.Value
		}
		return true
	})
	return v
}
func TestIncidentsWindowsResolvedDedupRevisionAndUTF8(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 0, 0, time.FixedZone("fixture", 3600))
	summary := `{"components":null}`
	incidents := incidentJSON(at, strings.Repeat("界", 3000))
	srv := server(t, &summary, &incidents)
	r, _, i := registered(t, srv.URL, time.Minute, 500)
	e, _, logs := emitter(t)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(r, e, store)
	// The lower checkpoint is exclusive; the upper source bound is inclusive.
	s.Now = func() time.Time { return at }
	run(t, s, i)
	if len(logs.records) != 1 {
		t.Fatalf("first checkpoint interval/dedup emitted %d", len(logs.records))
	}
	record := logs.records[0]
	if !record.Timestamp().Equal(at.UTC()) {
		t.Fatal("event time must be source updated_at")
	}
	if !utf8.ValidString(record.Body().AsString()) || len(record.Body().AsString()) > 8192 || len(record.Body().AsString()) < 8189 {
		t.Fatal("body not UTF8 byte-bounded")
	}
	flag := logValue(record, "cloudflare.status.update.truncated")
	if flag.Type() != attribute.STRING || flag.AsString() != "true" {
		t.Fatalf("truncation must be string true: %v", flag)
	}
	for key, want := range map[string]string{semconv.AttrEventName: "cloudflare.status.incident.update", "cloudflare.status.incident.id": "incident-alpha", "cloudflare.status.incident.name": "opaque-incident", "cloudflare.status.incident.status": "resolved", "cloudflare.status.incident.impact": "minor", "cloudflare.status.update.id": "update-alpha", "cloudflare.status.update.status": "resolved"} {
		if got := logValue(record, key); got.Type() != attribute.STRING || got.AsString() != want {
			t.Errorf("%s=%v", key, got)
		}
	}
	s.Now = func() time.Time { return at.Add(time.Minute) }
	run(t, s, i)
	if len(logs.records) != 2 || logs.records[1].Body().AsString() != "revision" || !logs.records[1].Timestamp().Equal(at.Add(time.Minute).UTC()) {
		t.Fatal("boundary revision omitted or old revision repeated")
	}
	if cp, _ := store.Get(i.Collector.Name()); !cp.Equal(at.Add(time.Minute).UTC()) {
		t.Fatal("normal checkpoint not advanced")
	}
}
func TestIncidentsWholeResponseRollbackAndEmpty(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	summary := `{"components":[]}`
	incidents := incidentJSON(at, "opaque-body")
	srv := server(t, &summary, &incidents)
	r, _, i := registered(t, srv.URL, time.Minute, 500)
	e, _, logs := emitter(t)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(r, e, store)
	s.Now = func() time.Time { return at.Add(time.Minute) }
	if err := store.Set(i.Collector.Name(), at); err != nil {
		t.Fatal(err)
	}
	good := incidents
	for _, bad := range []string{`{}`, `{"incidents":null}`, strings.Replace(good, `"updated_at":"`, `"updated_at":null,"unused":"`, 1), strings.Replace(good, `"body":"revision"`, `"body":17`, 1), strings.Replace(good, at.Add(time.Minute).Format(time.RFC3339Nano), "not-a-time", 1)} {
		incidents = bad
		if err := s.RunOnce(context.Background(), i); err == nil {
			t.Fatal("invalid whole response accepted")
		}
		if len(logs.records) != 0 {
			t.Fatal("valid first update leaked before invalid later row")
		}
		if cp, _ := store.Get(i.Collector.Name()); !cp.Equal(at) {
			t.Fatal("failed poll advanced checkpoint")
		}
	}
	incidents = `{"incidents":[]}`
	run(t, s, i)
	if cp, _ := store.Get(i.Collector.Name()); !cp.Equal(at.Add(time.Minute)) {
		t.Fatal("valid empty response did not advance checkpoint")
	}
}

// The real SDK export observer must veto the incident cursor on OTLP failure.
func TestIncidentExportFailureRetainsCursor(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	summary := `{"components":[]}`
	incidents := incidentJSON(at, "opaque-body")
	srv := server(t, &summary, &incidents)
	r, _, i := registered(t, srv.URL, time.Minute, 500)
	failing := true
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if failing && req.URL.Path == "/v1/logs" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	providers, err := telemetry.NewProviders(ctx, telemetry.ProviderOptions{Endpoint: sink.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = providers.Shutdown(shutdown)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(i.Collector.Name(), at); err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(r, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return at.Add(time.Minute) }
	if err := s.RunOnce(ctx, i); err == nil {
		t.Fatal("failed real OTLP export must fail poll")
	}
	if cp, _ := store.Get(i.Collector.Name()); !cp.Equal(at) {
		t.Fatal("export failure advanced cursor")
	}
	failing = false
	if err := s.RunOnce(ctx, i); err != nil {
		t.Fatal(err)
	}
	if cp, _ := store.Get(i.Collector.Name()); !cp.Equal(at.Add(time.Minute)) {
		t.Fatal("recovery did not advance cursor")
	}
}

func TestPublicHTTPRedirectDenied(t *testing.T) {
	redirected := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected <- struct{}{}
		fmt.Fprint(w, `{"components":[]}`)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.URL, http.StatusFound)
	}))
	defer origin.Close()
	r, components, _ := registered(t, origin.URL, time.Minute, 500)
	e, reader, _ := emitter(t)
	s := collector.NewScheduler(r, e, nil)
	if err := s.RunOnce(context.Background(), components); err == nil {
		t.Fatal("redirect must fail rather than poll a different origin")
	}
	if len(redirected) != 0 || len(points(t, reader)) != 0 {
		t.Fatal("redirect target was requested or a failed read published a snapshot")
	}
}

func TestPublicHTTPBoundsAndDisabledRegistration(t *testing.T) {
	cfg := config.Default()
	r := collector.NewRegistry()
	statuspage.Register(collector.Deps{Config: &cfg, Registry: r})
	if len(r.Entries()) != 0 {
		t.Fatal("default-disabled status collectors registered")
	}
	t.Setenv("CF2OTEL_STATUSPAGE__MAX_RESPONSE_BYTES", "256")
	t.Setenv("CF2OTEL_STATUSPAGE__TIMEOUT", "40ms")
	mode := "good"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch mode {
		case "oversize":
			fmt.Fprintf(w, `{"components":[],"padding":%q}`, strings.Repeat("x", 257))
		case "status":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "timeout":
			select {
			case <-req.Context().Done():
			case <-time.After(time.Second):
			}
		default:
			fmt.Fprint(w, `{"components":[{"name":"alpha","status":"major_outage","group":false}]}`)
		}
	}))
	defer srv.Close()
	registry, c, _ := registered(t, srv.URL, time.Minute, 500)
	e, reader, _ := emitter(t)
	s := collector.NewScheduler(registry, e, nil)
	run(t, s, c)
	want := points(t, reader)
	for _, bad := range []string{"oversize", "status", "timeout"} {
		mode = bad
		if err := s.RunOnce(context.Background(), c); err == nil {
			t.Errorf("%s must fail", bad)
		}
		if got := points(t, reader); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s changed prior snapshot", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RunOnce(ctx, c); err == nil {
		t.Fatal("canceled request accepted")
	}
}
