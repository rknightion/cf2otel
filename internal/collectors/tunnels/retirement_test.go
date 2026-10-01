package tunnels_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

// The reader retains synchronous gauge series across polls, just like the
// production cumulative reader. A new Buffer per poll cannot expose stale series.
func realEmitter(t *testing.T) (telemetry.Emitter, *sdkmetric.ManualReader, *exportedLogs) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	logs := &exportedLogs{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()); _ = lp.Shutdown(context.Background()) })
	return telemetry.NewEmitter(mp.Meter("fixture"), lp.Logger("fixture"), noop.NewTracerProvider().Tracer("fixture")), reader, logs
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

func TestRegisteredCumulativeGaugeRetirement(t *testing.T) {
	snapshot := `[]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"success":true,"result":%s}`, snapshot) }))
	defer server.Close()
	c := registered(t, cfapi.New(config.CloudflareConfig{APIBase: server.URL}))
	emitter, reader, _ := realEmitter(t)
	ctx := context.Background()
	collect := func() map[string]map[string]float64 {
		t.Helper()
		if err := c.Collect(ctx, emitter); err != nil {
			t.Fatal(err)
		}
		var out metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &out); err != nil {
			t.Fatal(err)
		}
		values := map[string]map[string]float64{}
		for _, scope := range out.ScopeMetrics {
			for _, m := range scope.Metrics {
				gauge, ok := m.Data.(metricdata.Gauge[float64])
				if !ok {
					t.Fatalf("not a gauge: %s", m.Name)
				}
				values[m.Name] = map[string]float64{}
				for _, p := range gauge.DataPoints {
					get := func(key string) string { v, _ := p.Attributes.Value(attribute.Key(key)); return v.AsString() }
					key := get(semconv.AttrTunnelID) + "/" + get(semconv.AttrTunnelStatus) + "/" + get(semconv.AttrTunnelColo) + "/" + get(semconv.AttrTunnelConnectorVersion)
					values[m.Name][key] = p.Value
				}
			}
		}
		return values
	}
	if got := collect(); len(got) != 0 {
		t.Fatalf("first empty snapshot fabricated gauges: %v", got)
	}
	snapshot = `[{"id":"tunnel-example","name":"example.com","status":"healthy","connections":[{"client_id":"connector-one","client_version":"2026.1","colo_name":"LHR"}]},{"id":"tunnel-other","name":"other.example.com","status":"healthy"}]`
	collect()
	snapshot = `[{"id":"tunnel-example","name":"example.com","status":"down","connections":[{"client_id":"connector-two","client_version":"2026.2","colo_name":"AMS"}]}]`
	values := collect()
	for metric, expected := range map[string]map[string]float64{
		semconv.MetricTunnelStatus:      {"tunnel-example/healthy//": 0, "tunnel-other/healthy//": 0, "tunnel-example/down//": 1},
		semconv.MetricTunnelConnections: {"tunnel-example//LHR/": 0, "tunnel-example//AMS/": 1},
		semconv.MetricTunnelConnectors:  {"tunnel-example///2026.1": 0, "tunnel-example///2026.2": 1},
	} {
		total := float64(0)
		for key, want := range expected {
			got, ok := values[metric][key]
			if !ok || got != want {
				t.Errorf("%s %s=%v present=%v; want %v (stale cumulative series)", metric, key, got, ok, want)
			}
		}
		for _, v := range values[metric] {
			total += v
		}
		if total != 1 {
			t.Errorf("%s current total=%v; want 1", metric, total)
		}
	}
	snapshot = `[]`
	for metric, points := range collect() {
		for key, value := range points {
			if value != 0 {
				t.Errorf("empty snapshot retained positive %s %s=%v", metric, key, value)
			}
		}
	}
}

// Reject at the emission edge after one real SDK export has been accepted.
type failSecondEvent struct {
	telemetry.Emitter
	calls int
	fail  bool
}

func (e *failSecondEvent) LogEvent(ctx context.Context, name, body string, at time.Time, severity otellog.Severity, attrs ...telemetry.Attr) error {
	e.calls++
	if e.fail && e.calls == 2 {
		return errors.New("fixture second event rejected")
	}
	return e.Emitter.LogEvent(ctx, name, body, at, severity, attrs...)
}
func logAttr(r sdklog.Record, key string) string {
	value := ""
	r.WalkAttributes(func(a attribute.KeyValue) bool {
		if string(a.Key) == key {
			value = a.Value.AsString()
		}
		return true
	})
	return value
}
func TestRegisteredPartialEventRetryKeepsDedupeKey(t *testing.T) {
	status := "healthy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"success":true,"result":[{"id":"tunnel-one","name":"one.example.com","status":%q},{"id":"tunnel-two","name":"two.example.com","status":%q}]}`, status, status)
	}))
	defer server.Close()
	c := registered(t, cfapi.New(config.CloudflareConfig{APIBase: server.URL}))
	emitter, _, logs := realEmitter(t)
	edge := &failSecondEvent{Emitter: emitter}
	ctx := context.Background()
	if err := c.Collect(ctx, edge); err != nil {
		t.Fatal(err)
	}
	status = "down"
	edge.fail = true
	if err := c.Collect(ctx, edge); err == nil {
		t.Fatal("second event rejection must fail")
	}
	if len(logs.records) != 1 {
		t.Fatalf("accepted %d events before failure; want 1", len(logs.records))
	}
	first := logs.records[0]
	// Retry the same state and reject the second transition again.
	edge.calls = 0
	if err := c.Collect(ctx, edge); err == nil {
		t.Fatal("repeated second event rejection must fail")
	}
	if len(logs.records) != 2 || !logs.records[1].Timestamp().Equal(first.Timestamp()) {
		t.Error("retry changed the accepted event's frozen dedupe timestamp")
	}
	// A different poll must first finish the pending down transitions, then emit
	// down->healthy, not silently forget the second down transition.
	status = "healthy"
	edge.fail = false
	if err := c.Collect(ctx, edge); err != nil {
		t.Fatal(err)
	}
	downs, ups := map[string]time.Time{}, map[string]bool{}
	for _, r := range logs.records {
		id, state := logAttr(r, semconv.AttrTunnelID), logAttr(r, semconv.AttrTunnelStatus)
		switch state {
		case "down":
			if old, ok := downs[id]; ok && !old.Equal(r.Timestamp()) {
				t.Errorf("retry changed dedupe timestamp for %s: %s -> %s", id, old, r.Timestamp())
			}
			downs[id] = r.Timestamp()
			if logAttr(r, semconv.AttrTunnelPreviousStatus) != "healthy" {
				t.Error("wrong previous state for pending transition")
			}
		case "healthy":
			ups[id] = true
			if logAttr(r, semconv.AttrTunnelPreviousStatus) != "down" {
				t.Error("wrong previous state after pending snapshot")
			}
		}
	}
	for _, id := range []string{"tunnel-one", "tunnel-two"} {
		if !downs[id].Equal(first.Timestamp()) {
			t.Errorf("lost frozen down transition timestamp for %s", id)
		}
		if !ups[id] {
			t.Errorf("lost reconciled healthy transition for %s", id)
		}
	}
	before := len(logs.records)
	if err := c.Collect(ctx, edge); err != nil {
		t.Fatal(err)
	}
	if len(logs.records) != before {
		t.Fatal("committed unchanged snapshot emitted extra events")
	}
}
