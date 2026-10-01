package selfobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type sample struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type fakeEmitter struct {
	mu                           sync.Mutex
	gauges, counters, histograms []sample
}

func (e *fakeEmitter) Gauge(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.gauges = append(e.gauges, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) Counter(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counters = append(e.counters, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) Histogram(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.histograms = append(e.histograms, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (e *fakeEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

// Exercise the registered collector with the real identity index and SDK reader:
// an unchanged poll must not add the cumulative snapshot a second time.
func TestRegisteredIdentityOutcomeCounterDeltas(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })
	emitter := telemetry.NewEmitter(provider.Meter("test"), nil, nil)
	index, err := identity.New(time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	index.Observe(identity.Login{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "invented-user-one", RayID: "invented-one", At: now})
	index.Observe(identity.Login{ClientIP: "192.0.2.2", Host: "example.com", UserEmail: "invented-user-one", RayID: "invented-two", At: now})
	index.Observe(identity.Login{ClientIP: "192.0.2.2", Host: "example.com", UserEmail: "invented-user-two", RayID: "invented-three", At: now})
	lookups := func(matched, unmatched, ambiguous int) {
		for range matched {
			index.Lookup("192.0.2.1", "example.com", now)
		}
		for range unmatched {
			index.Lookup("192.0.2.3", "example.com", now)
		}
		for range ambiguous {
			index.Lookup("192.0.2.2", "example.com", now)
		}
	}
	stats := New(emitter, "test", "test")
	stats.SetIdentityStats(index.Stats)
	registry := collector.NewRegistry()
	cfg := config.Default()
	Register(collector.Deps{Config: &cfg, Registry: registry, SelfObs: NewCollector(stats)})
	entries := registry.Entries()
	if len(entries) != 1 {
		t.Fatalf("registered collectors = %d", len(entries))
	}
	run := entries[0].Collector.(collector.SnapshotCollector)
	assertPoll := func(want map[string]float64) {
		t.Helper()
		if err := run.Collect(ctx, emitter); err != nil {
			t.Fatal(err)
		}
		var data metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &data); err != nil {
			t.Fatal(err)
		}
		got := map[string]float64{}
		for _, scope := range data.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if metric.Name != semconv.MetricIdentityOutcomes {
					continue
				}
				if metric.Unit != "{request}" {
					t.Fatalf("outcome unit = %q", metric.Unit)
				}
				sum, ok := metric.Data.(metricdata.Sum[float64])
				if !ok || !sum.IsMonotonic {
					t.Fatalf("outcomes are not a monotonic counter: %T", metric.Data)
				}
				for _, point := range sum.DataPoints {
					outcome, ok := point.Attributes.Value(attribute.Key(semconv.AttrIdentityOutcome))
					if !ok || point.Attributes.Len() != 1 {
						t.Fatalf("outcome attributes = %v", point.Attributes)
					}
					got[outcome.AsString()] = point.Value
				}
			}
		}
		for outcome, value := range want {
			if got[outcome] != value {
				t.Fatalf("identity outcome %s = %v, want %v (all: %v)", outcome, got[outcome], value, got)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("identity outcomes = %v, want %v", got, want)
		}
	}
	lookups(1, 1, 1)
	assertPoll(map[string]float64{"matched": 1, "unmatched": 1, "ambiguous": 1})
	assertPoll(map[string]float64{"matched": 1, "unmatched": 1, "ambiguous": 1})
	lookups(1, 2, 1)
	assertPoll(map[string]float64{"matched": 2, "unmatched": 3, "ambiguous": 2})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := run.Collect(ctx, emitter); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	assertPoll(map[string]float64{"matched": 2, "unmatched": 3, "ambiguous": 2})
	// A replacement index starts at zero without subtracting from the counter.
	replacement, err := identity.New(time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	stats.SetIdentityStats(replacement.Stats)
	assertPoll(map[string]float64{"matched": 2, "unmatched": 3, "ambiguous": 2})
	replacement.Lookup("192.0.2.3", "example.com", now)
	assertPoll(map[string]float64{"matched": 2, "unmatched": 4, "ambiguous": 2})
	// A source can already have outcomes before being attached.
	preloaded, err := identity.New(time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		preloaded.Lookup("192.0.2.3", "example.com", now)
	}
	stats.SetIdentityStats(preloaded.Stats)
	assertPoll(map[string]float64{"matched": 2, "unmatched": 14, "ambiguous": 2})
}

func TestPollAndCollect(t *testing.T) {
	e := &fakeEmitter{}
	s := New(e, "0.1.0", "abc")
	s.Expect("httpreq.events")
	s.SetIdentityStats(func() identity.Stats { return identity.Stats{Matched: 2, Unmatched: 3, Ambiguous: 1} })
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if err := s.Poll(context.Background(), "access.logins", 250*time.Millisecond, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(context.Background(), "httpreq.events", time.Second, context.DeadlineExceeded, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(context.Background(), "logs", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(context.Background(), "traces", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	s.Checkpoint("access.logins", now.Add(-time.Minute))
	if err := s.Collect(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(e.counters) != 7 || len(e.histograms) != 2 {
		t.Fatalf("counters=%d histograms=%d", len(e.counters), len(e.histograms))
	}
	if len(e.gauges) != 4 {
		t.Fatalf("gauges=%d", len(e.gauges))
	}
	identityValues := map[string]float64{}
	for _, counter := range e.counters {
		if counter.name == semconv.MetricIdentityOutcomes && len(counter.attrs) == 1 {
			identityValues[counter.attrs[0].Value] = counter.value
		}
	}
	if identityValues["matched"] != 2 || identityValues["unmatched"] != 3 || identityValues["ambiguous"] != 1 {
		t.Fatalf("identity outcome counters: %+v", identityValues)
	}
	missing := false
	for _, g := range e.gauges {
		if g.name == "cf2otel.scrape.last_success_timestamp" && g.value == 0 && len(g.attrs) == 1 && g.attrs[0].Value == "httpreq.events" {
			missing = true
		}
	}
	if !missing {
		t.Fatal("missing collector did not produce sentinel last-success gauge")
	}
}
